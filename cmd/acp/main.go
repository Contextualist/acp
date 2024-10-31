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

	sinfo := pnet.SelfInfo{ChanName: conf.ID}
	strategy, errs := tryEach(conf.Strategy, func(name string) (s string, err error) {
		var d stream.Dialer
		if d, err = stream.GetDialer(name); err != nil {
			return
		}
		if err = d.Init(*conf); err != nil {
			return "", fmt.Errorf("failed to init dialer %s: %w", name, err)
		}
		d.SetInfo(&sinfo)
		return name, nil
	})
	sinfo.Strategy = strategy
	if len(strategy) == 0 {
		checkErr(fmt.Errorf("none of the dialers from the strategy is available: %w", errors.Join(errs...)))
		return
	}

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

	var status interface {
		Next(tea.Model) string
		Logf(string, ...any)
	}
	if len(filenames) > 0 {
		var s io.WriteCloser
		strategyFinal := strategyConsensus(strategy, info.Strategy)
		s, err = tryUntil(strategyFinal, func(dn string) (io.WriteCloser, error) { return must(stream.GetDialer(dn)).IntoSender(ctx, *info) })
		if !checkErr(err) {
			return
		}
		s, status = monitor(s)
		logger.Debugf("sending...")
		err = sendFiles(filenames, s, status.Logf)
	} else {
		var s io.ReadCloser
		strategyFinal := strategyConsensus(info.Strategy, strategy)
		s, err = tryUntil(strategyFinal, func(dn string) (io.ReadCloser, error) { return must(stream.GetDialer(dn)).IntoReceiver(ctx, *info) })
		if !checkErr(err) {
			return
		}
		s, status = monitor(s)
		logger.Debugf("receiving...")
		err = receiveFiles(s)
	}

	if !*debug {
		exitStatement = status.Next(loggerModel) // save in-transit log, if any
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
