package updater

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

type zipEntry struct {
	name string
	data string
	mode os.FileMode
}

func makeZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(entry.mode)
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractArchive(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipEntry
		code    string
	}{
		{name: "zip slip", entries: []zipEntry{{name: "../outside", data: "x", mode: 0644}}, code: "bad_archive"},
		{name: "symlink escape", entries: []zipEntry{{name: "escape", data: "../../outside", mode: os.ModeSymlink | 0777}}, code: "bad_archive"},
		{name: "write through symlink", entries: []zipEntry{{name: "link", data: "dir", mode: os.ModeSymlink | 0777}, {name: "link/file", data: "x", mode: 0644}}, code: "bad_archive"},
		{name: "valid symlink and executable", entries: []zipEntry{{name: "bin/app", data: "binary", mode: 0755}, {name: "app-link", data: "bin/app", mode: os.ModeSymlink | 0777}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "out")
			err := extractArchive(makeZip(t, test.entries), destination)
			if ErrorCode(err) != test.code {
				t.Fatalf("error=%v code=%q want=%q", err, ErrorCode(err), test.code)
			}
			if test.code == "" {
				info, err := os.Stat(filepath.Join(destination, "bin", "app"))
				if err != nil || info.Mode().Perm()&0111 == 0 {
					t.Fatalf("executable mode not preserved: %v, %v", info, err)
				}
				target, err := os.Readlink(filepath.Join(destination, "app-link"))
				if err != nil || target != "bin/app" {
					t.Fatalf("symlink not preserved: %q, %v", target, err)
				}
			}
		})
	}
}
