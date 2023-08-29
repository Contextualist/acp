package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mouuff/go-rocket-update/pkg/provider"
	"github.com/mouuff/go-rocket-update/pkg/updater"
)

const RepoName = "github.com/contextualist/acp"
const ExeName = "acp"

func tryUpdate(exe string, repo string, currTag string) error {
	u := &updater.Updater{
		Provider: &provider.Github{
			RepositoryURL: repo,
			ArchiveName:   getArchiveName(exe),
		},
		ExecutableName: exe,
		Version:        currTag,
		PostUpdateFunc: func(u *updater.Updater) (updater.UpdateStatus, error) {
			p, _ := u.GetExecutable()
			if p != "" {
				_ = os.Remove(p + ".old")
				_ = os.Rename(p+".old", filepath.Join(os.TempDir(), exe+".old"))
			}
			return updater.Updated, nil
		},
	}

	isOutdate, err := u.CanUpdate()
	if err != nil {
		return fmt.Errorf("failed to check update: %w", err)
	}
	if !isOutdate {
		fmt.Println("acp is up to date")
		return nil
	}
	latest, err := u.GetLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check update: %w", err)
	}
	fmt.Printf("Found latest version %s\n", latest)

	_, err = u.Update()
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	return nil
}

func getArchiveName(exe string) string {
	gos, garch, ext := runtime.GOOS, runtime.GOARCH, ".tar.gz"
	gos = strings.ToUpper(gos[0:1]) + gos[1:]
	if garch == "amd64" {
		garch = "x86_64"
	}
	if gos == "Windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s%s", exe, gos, garch, ext)
}
