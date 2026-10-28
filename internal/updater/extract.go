package updater

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractArchive(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return coded("bad_archive", err)
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0700); err != nil {
		return coded("bad_archive", err)
	}
	for _, file := range reader.File {
		name, err := safeArchivePath(file.Name)
		if err != nil {
			return coded("bad_archive", err)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			data, err := readZipFile(file, 16<<10)
			if err != nil {
				return coded("bad_archive", err)
			}
			target := string(data)
			if filepath.IsAbs(target) {
				return coded("bad_archive", fmt.Errorf("absolute symlink target"))
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(name), target))
			if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
				return coded("bad_archive", fmt.Errorf("symlink escapes archive"))
			}
		}
	}
	for _, file := range reader.File {
		name, _ := safeArchivePath(file.Name)
		path := filepath.Join(destination, name)
		if err := ensureNoSymlinkParent(destination, filepath.Dir(path)); err != nil {
			return coded("bad_archive", err)
		}
		mode := file.Mode()
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, mode.Perm()); err != nil {
				return coded("bad_archive", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return coded("bad_archive", err)
		}
		if mode&os.ModeSymlink != 0 {
			data, err := readZipFile(file, 16<<10)
			if err != nil {
				return coded("bad_archive", err)
			}
			if err := os.Symlink(string(data), path); err != nil {
				return coded("bad_archive", err)
			}
			continue
		}
		input, err := file.Open()
		if err != nil {
			return coded("bad_archive", err)
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
		if err != nil {
			input.Close()
			return coded("bad_archive", err)
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		input.Close()
		if copyErr != nil {
			return coded("bad_archive", copyErr)
		}
		if closeErr != nil {
			return coded("bad_archive", closeErr)
		}
		if err := os.Chmod(path, mode.Perm()); err != nil {
			return coded("bad_archive", err)
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	if strings.ContainsRune(name, '\x00') || filepath.IsAbs(name) || strings.Contains(name, `\`) {
		return "", fmt.Errorf("unsafe archive path")
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path")
	}
	return cleaned, nil
}

func ensureNoSymlinkParent(root, parent string) error {
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes extraction root")
	}
	current := root
	for _, part := range splitPath(filepath.ToSlash(relative)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive writes through symlink")
		}
	}
	return nil
}

func readZipFile(file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return readLimited(reader, limit)
}
