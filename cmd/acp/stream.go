package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/pgzip"
)

func sendFiles(filenames []string, to io.WriteCloser) (err error) {
	defer to.Close()
	z := pgzip.NewWriter(to)
	defer z.Close()

	if len(filenames) == 1 && filenames[0] == "-" {
		_, err = io.Copy(z, os.Stdin)
		return
	}

	tz := tar.NewWriter(z)
	defer tz.Close()

	for _, fname := range filenames {
		fname, err = filepath.Abs(fname)
		if err != nil {
			return err
		}
		err = tarWalk(fname, tz)
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
	}
	return
}

func receiveFiles(from io.ReadCloser) (err error) {
	defer from.Close()
	z, err := pgzip.NewReader(from)
	if err != nil {
		return
	}

	if *destination == "-" {
		_, err = io.Copy(os.Stdout, z)
		return
	}

	dest, destFile, err := parseDest(*destination)
	if err != nil {
		return
	}

	defer z.Close()
	tz := tar.NewReader(z)

	var theFile string
	var hdr *tar.Header
	for {
		hdr, err = tz.Next()
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			return
		}

		if !strings.ContainsRune(filepath.Clean(hdr.Name), os.PathSeparator) {
			if theFile == "" { // capture the name if there's only one toplevel file/dir
				theFile = hdr.Name
			} else {
				theFile = "N/A"
			}
		}
		err = untarFile(hdr, tz, dest)
		if err != nil {
			return fmt.Errorf("untar: %w", err)
		}
	}

	if destFile != "" {
		if theFile == "" {
			return os.Remove(dest)
		}
		if theFile != "N/A" { // one file or dir
			err = os.Rename(filepath.Join(dest, theFile), destFile)
			if err != nil {
				return
			}
			return os.Remove(dest)
		}
		err = os.Rename(dest, destFile)
		if err != nil {
			destFile = dest // fail to rename, we are OK with the tmpdir
		}
		logger.Infof("received more than one file or dir, saved to dir %#v", destFile)
	}
	return
}

func parseDest(d string) (dest, destFile string, err error) {
	if d == "" {
		return d, "", nil
	}
	if info, err := os.Stat(d); err == nil && info.Mode().IsDir() { // an existed dest dir
		return d, "", nil
	}
	p := filepath.Dir(d)
	if info, err := os.Stat(p); err == nil && info.Mode().IsDir() { // first receive to a tempdir, then mv to destFile
		p, err = os.MkdirTemp(p, "acp-tmp.")
		return p, d, err
	}
	return "", "", fmt.Errorf("no such file or directory: %s", d)
}
