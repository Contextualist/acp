package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/contextualist/acp/pkg/config"
	"github.com/contextualist/acp/pkg/pnet"
	"github.com/contextualist/acp/pkg/stream"
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
		checkErr(config.Setup(*doSetupWith))
		return
	}

	filenames := flag.Args()
	conf := config.MustGetConfig()
	conf.ApplyDefault()

	ctx, userCancel := context.WithCancel(context.Background())
	logger = tui.NewLoggerControl(*debug)
	loggerModel := tui.NewLoggerModel(logger)
	go transfer(ctx, conf, filenames, loggerModel)
	tui.RunProgram(loggerModel, userCancel, *destination == "-")
}

func transfer(ctx context.Context, conf *config.Config, filenames []string, loggerModel tea.Model) {
	pnet.SetLogger(logger)
	stream.SetLogger(logger)
	defer logger.End()

	dialer, _ := stream.GetDialer("tcp_punch")
	err := dialer.Init(*conf)
	if !checkErr(err) {
		return
	}

	sinfo := pnet.SelfInfo{ChanName: conf.ID}
	dialer.SetInfo(&sinfo)
	info, err := pnet.ExchangeConnInfo(
		ctx,
		conf.Server+"/v2/exchange",
		&sinfo,
		conf.Ports[0],
		conf.UseIPv6,
	)
	if !checkErr(err) {
		return
	}

	var status interface{ Next(tea.Model) }
	if len(filenames) > 0 {
		var s io.WriteCloser
		s, err = dialer.IntoSender(ctx, *info)
		if !checkErr(err) {
			return
		}
		s, status = monitor(s)
		logger.Debugf("sending...")
		err = sendFiles(filenames, s)
	} else {
		var s io.ReadCloser
		s, err = dialer.IntoReceiver(ctx, *info)
		if !checkErr(err) {
			return
		}
		s, status = monitor(s)
		logger.Debugf("receiving...")
		err = receiveFiles(s)
	}

	if !*debug {
		status.Next(loggerModel)
	}
	checkErr(err)
}

func monitor[T io.Closer](s T) (T, *tui.StatusControl[T]) {
	var status *tui.StatusControl[T]
	if !*debug {
		status = tui.NewStatusControl[T]()
		s = status.Monitor(s)
		logger.Next(tui.NewStatusModel(status))
	}
	return s, status
}

func checkErr(err error) bool {
	if err == nil {
		return true
	}
	if !errors.Is(err, context.Canceled) {
		exitStatement = err.Error()
	}
	return false
}
