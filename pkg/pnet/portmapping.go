package pnet

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/huin/goupnp/dcps/internetgateway2"
	"golang.org/x/sync/errgroup"
)

type routerClient interface {
	AddPortMappingCtx(
		ctx context.Context,
		NewRemoteHost string,
		NewExternalPort uint16,
		NewProtocol string,
		NewInternalPort uint16,
		NewInternalClient string,
		NewEnabled bool,
		NewPortMappingDescription string,
		NewLeaseDuration uint32,
	) error

	LocalAddr() net.IP

	GetExternalIPAddress() (string, error)
}

func AddPortMapping(ctx context.Context, ports ...int) error {
	client, err := pickRouterClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to find a router client: %w", err)
	}

	var errs []error
	for _, port := range ports {
		err = client.AddPortMappingCtx(ctx, "", uint16(port), "TCP", uint16(port), client.LocalAddr().String(), true, "acp", 60)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to add port mapping: %w", err))
		}
	}

	return errors.Join(errs...)
}

func pickRouterClient(ctx context.Context) (routerClient, error) {
	tasks, _ := errgroup.WithContext(ctx)
	chFound := make(chan routerClient)
	tasks.Go(discoverWith(internetgateway2.NewWANIPConnection1ClientsCtx, ctx, chFound))
	tasks.Go(discoverWith(internetgateway2.NewWANIPConnection2ClientsCtx, ctx, chFound))
	tasks.Go(discoverWith(internetgateway2.NewWANPPPConnection1ClientsCtx, ctx, chFound))

	chErr := make(chan error)
	go func() {
		err := tasks.Wait()
		close(chFound)
		if err == nil {
			err = errors.New("no port mapping service found")
		}
		chErr <- err
		close(chErr)
	}()

	c, ok := <-chFound
	if !ok { // no successful client
		return nil, <-chErr
	}
	go drain(chFound)
	go drain(chErr)
	return c, nil
}

func discoverWith[T routerClient](newc func(context.Context) ([]T, []error, error), ctx context.Context, chFound chan routerClient) func() error {
	return func() (err error) {
		cs, _, err := newc(ctx)
		for _, c := range cs {
			chFound <- c
		}
		return
	}
}

func drain[T any](ch chan T) {
	for range ch {
	}
}
