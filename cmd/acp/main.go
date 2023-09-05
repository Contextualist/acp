package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/contextualist/acp/pkg/pnet"
	"github.com/contextualist/acp/pkg/tui"
)

const UsageBrief = `Usage:
  # sender
  acp path/to/files

  # receiver, to $(pwd)
  acp
  # or receive to/as specified target
  acp -d path/to/target
`

var buildTag string // build-time injected

var (
	destination = flag.String("d", ".", "Save files to target directory / rename received file")
	debug       = flag.Bool("debug", false, "Enable debug logging")
	doSetup     = flag.Bool("setup", false, "Initialize config or display current config")
	doSetupWith = flag.String("setup-with", "", "Initialize config with the specified value")
	doUpdate    = flag.Bool("update", false, "Update itself if a new version exists")
	showVersion = flag.Bool("version", false, "Print version and exit")
)

var logger tui.LoggerControl

var exitStatement string

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s (%s)\n%s\nOptions:\n", os.Args[0], buildTag, UsageBrief)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(buildTag)
		return
	}

	defer func() {
		if exitStatement != "" {
			fmt.Fprintln(os.Stderr, exitStatement)
			os.Exit(1)
		}
	}()

	if *doUpdate {
		checkErr(tryUpdate(ExeName, RepoName, fmt.Sprintf("v%s", buildTag)))
		return
	}
	if *doSetup || *doSetupWith != "" {
		checkErr(setup(*doSetupWith))
		return
	}

	filenames := flag.Args()
	conf := mustGetConfig()
	conf.applyDefault()

	ctx, userCancel := context.WithCancel(context.Background())
	logger = tui.NewLoggerControl(*debug)
	loggerModel := tui.NewLoggerModel(logger)
	go transfer(ctx, conf, filenames, loggerModel)
	tui.RunProgram(loggerModel, userCancel, *destination == "-")
}

func transfer(ctx context.Context, conf *Config, filenames []string, loggerModel tea.Model) {
	defer logger.End()

	conn, err := pnet.HolePunching(
		ctx,
		conf.Server+"/v2/exchange",
		conf.ID,
		len(filenames) > 0,
		pnet.HolePunchingOptions{
			UseIPv6: conf.UseIPv6,
			Ports:   conf.Ports,
			UPnP:    conf.UPnP,
		},
		logger,
	)
	if errors.Is(err, context.Canceled) || !checkErr(err) {
		return
	}

	psk, err := base64.StdEncoding.DecodeString(conf.PSK)
	if !checkErr(err) {
		return
	}
	conn, err = encrypted(conn, psk)
	if !checkErr(err) {
		return
	}

	stream, _ := conn.(io.ReadWriteCloser)
	var status *tui.StatusControl
	if !*debug {
		status = tui.NewStatusControl()
		stream = status.Monitor(stream)
		logger.Next(tui.NewStatusModel(status))
	}

	if len(filenames) > 0 {
		logger.Debugf("sending...")
		err = sendFiles(filenames, stream)
	} else {
		logger.Debugf("receiving...")
		err = receiveFiles(stream)
	}

	if !*debug {
		status.Next(loggerModel)
	}
	checkErr(err)
}

func checkErr(err error) bool {
	if err == nil {
		return true
	}
	exitStatement = err.Error()
	return false
}
