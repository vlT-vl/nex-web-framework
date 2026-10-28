package updater

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func platformLayout() string {
	if runtime.GOOS == "darwin" {
		return "bundle"
	}
	return "binary"
}

func (u *Updater) layout() string {
	if u.cfg.Layout != "" {
		return u.cfg.Layout
	}
	return platformLayout()
}

func installTarget() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		parts := strings.Split(filepath.Clean(executable), string(filepath.Separator))
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.HasSuffix(strings.ToLower(parts[i]), ".app") {
				prefix := string(filepath.Separator)
				if !filepath.IsAbs(executable) {
					prefix = ""
				}
				return filepath.Join(append([]string{prefix}, parts[:i+1]...)...), nil
			}
		}
	}
	return executable, nil
}

func validateLayout(root string, asset Asset) (string, string, error) {
	if asset.Layout != "bundle" && asset.Layout != "binary" {
		return "", "", fmt.Errorf("unsupported layout")
	}
	if asset.Layout == "bundle" {
		entries, err := os.ReadDir(root)
		if err != nil {
			return "", "", err
		}
		var bundle string
		for _, entry := range entries {
			if entry.Name() == "__MACOSX" {
				continue
			}
			if bundle != "" || !entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
				return "", "", fmt.Errorf("archive must contain exactly one top-level app bundle")
			}
			bundle = filepath.Join(root, entry.Name())
		}
		if bundle == "" {
			return "", "", fmt.Errorf("app bundle is missing")
		}
		launch, err := bundleExecutable(bundle, asset.Entry)
		if err != nil {
			return "", "", err
		}
		relative, _ := filepath.Rel(bundle, launch)
		return bundle, relative, nil
	}
	if asset.Entry != "" {
		entry, err := safeArchivePath(asset.Entry)
		if err != nil {
			return "", "", err
		}
		path := filepath.Join(root, entry)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("binary entry is missing")
		}
		return path, "", nil
	}
	var candidate string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			if candidate != "" {
				return fmt.Errorf("archive contains more than one file")
			}
			candidate = path
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if candidate == "" {
		return "", "", fmt.Errorf("binary is missing")
	}
	return candidate, "", nil
}

func bundleExecutable(bundle, entry string) (string, error) {
	if entry != "" {
		cleaned, err := safeArchivePath(entry)
		if err != nil {
			return "", err
		}
		if strings.HasSuffix(strings.ToLower(cleaned), ".app") {
			cleaned = strings.TrimPrefix(cleaned, filepath.Base(bundle)+string(filepath.Separator))
		}
		candidate := filepath.Join(bundle, cleaned)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	directory := filepath.Join(bundle, "Contents", "MacOS")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	var candidate string
	for _, item := range entries {
		info, err := item.Info()
		if err != nil {
			return "", err
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			if candidate != "" {
				return "", fmt.Errorf("bundle contains multiple executables")
			}
			candidate = filepath.Join(directory, item.Name())
		}
	}
	if candidate == "" {
		return "", fmt.Errorf("bundle executable is missing")
	}
	return candidate, nil
}
