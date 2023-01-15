package main

// Adapted from the unexported functions in pre-v4 of mholt/archiver
// Will consider moving away from vendoring once v4 is stablized

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func tarWalk(source string, t *tar.Writer) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("%s: stat: %w", source, err)
	}
	sourceIsDir := sourceInfo.IsDir()
	return filepath.Walk(source, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("traversing %s: %w", fpath, err)
		}
		if info == nil {
			return fmt.Errorf("no file info for %s", fpath)
		}
		// build the name to be used within the archive
		relName, err := nameInArchive(sourceIsDir, source, fpath)
		if err != nil {
			return err
		}
		var file io.ReadCloser
		if info.Mode().IsRegular() {
			file, err = os.Open(fpath)
			if err != nil {
				return fmt.Errorf("%s: opening: %w", fpath, err)
			}
			defer file.Close()
		}
		err = addFile(t, info, relName, file)
		if err != nil {
			return fmt.Errorf("%s: writing: %v", fpath, err)
		}
		return nil
	})
}

func nameInArchive(srcIsDir bool, src, fpath string) (string, error) {
	name := filepath.Base(fpath) // start with the file or dir name
	if srcIsDir {
		// preserve internal directory structure; that's the path components
		// between the source directory's leaf and this file's leaf
		dir, err := filepath.Rel(filepath.Dir(src), filepath.Dir(fpath))
		if err != nil {
			return "", err
		}
		// prepend the internal directory structure to the leaf name,
		// and convert path separators to forward slashes as per spec
		name = filepath.Join(filepath.ToSlash(dir), name)
	}
	return name, nil
}

type namedFileInfo struct {
	os.FileInfo
	name string
}

func (fi namedFileInfo) Name() string {
	return fi.name
}

func addFile(w *tar.Writer, info os.FileInfo, name string, file io.Reader) error {
	var linkTarget string
	if info.Mode()&os.ModeSymlink != 0 {
		var err error
		linkTarget, err = os.Readlink(name)
		if err != nil {
			return fmt.Errorf("%s: readlink: %v", name, err)
		}
	}

	hdr, err := tar.FileInfoHeader(namedFileInfo{info, name}, filepath.ToSlash(linkTarget))
	if err != nil {
		return fmt.Errorf("%s: making header: %v", name, err)
	}
	err = w.WriteHeader(hdr)
	if err != nil {
		return fmt.Errorf("%s: writing header: %v", hdr.Name, err)
	}

	if info.IsDir() {
		return nil // directories have no contents
	}
	if hdr.Typeflag == tar.TypeReg {
		_, err := io.Copy(w, file)
		if err != nil {
			return fmt.Errorf("%s: copying contents: %v", name, err)
		}
	}
	return nil
}

func untarFile(hdr *tar.Header, f io.Reader, dest string) error {
	to := filepath.Join(dest, hdr.Name)
	switch hdr.Typeflag {
	case tar.TypeDir:
		return mkdir(to)
	case tar.TypeReg, tar.TypeRegA, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return writeNewFile(to, f, hdr.FileInfo().Mode())
	case tar.TypeSymlink:
		return writeNewSymbolicLink(to, hdr.Linkname)
	case tar.TypeLink:
		return writeNewHardLink(to, filepath.Join(dest, hdr.Linkname))
	case tar.TypeXGlobalHeader:
		return nil // ignore the pax global header from git-generated tarballs
	default:
		return fmt.Errorf("%s: unknown type flag: %c", hdr.Name, hdr.Typeflag)
	}
}

func mkdir(dirPath string) error {
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory: %w", dirPath, err)
	}
	return nil
}

func writeNewFile(fpath string, in io.Reader, fm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}
	out, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("%s: creating new file: %w", fpath, err)
	}
	defer out.Close()
	err = out.Chmod(fm)
	if err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("%s: changing file mode: %w", fpath, err)
	}
	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("%s: writing file: %w", fpath, err)
	}
	return nil
}

func writeNewSymbolicLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}
	err = os.Symlink(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making symbolic link for: %w", fpath, err)
	}
	return nil
}

func writeNewHardLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}
	err = os.Link(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making hard link for: %w", fpath, err)
	}
	return nil
}
