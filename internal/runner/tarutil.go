package runner

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// TarPath streams a tar of a host file or directory with entries named
// like `docker cp SRC -` does: the base name of src, then its contents.
func TarPath(src string) (io.ReadCloser, error) {
	if _, err := os.Lstat(src); err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		parent := filepath.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			link := ""
			if info.Mode()&fs.ModeSymlink != 0 {
				if link, err = os.Readlink(p); err != nil {
					return err
				}
			}
			hdr, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(parent, p)
			hdr.Name = filepath.ToSlash(rel)
			if d.IsDir() {
				hdr.Name += "/"
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				f, err := os.Open(p)
				if err != nil {
					return err
				}
				_, err = io.Copy(tw, f)
				f.Close()
				return err
			}
			return nil
		})
		if err == nil {
			err = tw.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr, nil
}

// Untar extracts a tar stream into dir. Entries can't escape dir.
func Untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean("/" + hdr.Name)
		if name == "/" {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if !strings.HasPrefix(dst, filepath.Clean(dir)+string(filepath.Separator)) && filepath.Clean(dir) != "/" {
			return fmt.Errorf("tar entry %q escapes the destination", hdr.Name)
		}
		mode := fs.FileMode(hdr.Mode) & fs.ModePerm
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, mode|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dst)
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dst)
			if err := os.Symlink(hdr.Linkname, dst); err != nil {
				return err
			}
		case tar.TypeLink:
			target := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+hdr.Linkname)))
			_ = os.Remove(dst)
			if err := os.Link(target, dst); err != nil {
				return err
			}
		}
	}
}
