package pnet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestExchangeConnInfoProto(t *testing.T) {
	defaultLogger = &testLogger{t}
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	chRecvOrErr := make(chan readerOrError)
	go func() { chRecvOrErr <- readerOrError{ReadCloser: downR}; close(chRecvOrErr) }()

	sinfo0 := SelfInfo{PriAddr: "127.0.0.1:30001", ChanName: "test-exchange-proto", NPlan: 1}
	cInfo0 := PeerInfo{Laddr: sinfo0.PriAddr, PeerAddrs: []AddrPair{{"127.0.0.1:30002", "80.80.80.80:30003"}}, PeerNPlan: 1}
	go func() { // mock server protocol
		clientData, err := receivePacket(upR)
		if err != nil {
			t.Errorf("error on receiving client data: %v", err)
		}
		var sinfo SelfInfo
		err = json.Unmarshal(clientData, &sinfo)
		if err != nil {
			t.Errorf("error on parsing client data: %v", err)
		}
		if sinfo != sinfo0 {
			t.Errorf("unexpected client data: %v", sinfo)
		}
		err = sendPacket(downW, must(json.Marshal(&cInfo0)))
		if err != nil {
			t.Errorf("error on replying to client: %v", err)
		}
	}()

	cInfo, err := exchangeConnInfoProto(context.Background(), upW, chRecvOrErr, &sinfo0, nil)
	if err != nil {
		t.Fatalf("exchange proto: %v", err)
	}
	if !reflect.DeepEqual(*cInfo, cInfo0) {
		t.Fatalf("PeerInfo from exchange proto not matched: expect: %+v, got: %+v", cInfo0, *cInfo)
	}
}

func TestExchangeConnInfo(t *testing.T) {
	defaultLogger = &testLogger{t}
	id0 := "test-exchange"
	ra, rb := "80.80.80.80:30011", "80.80.80.80:30012"
	nplan := 1
	ch1, ch2 := make(chan []byte), make(chan []byte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientData, err := receivePacket(r.Body)
		if err != nil {
			t.Errorf("error on receiving client data: %v", err)
		}
		var sinfo SelfInfo
		err = json.Unmarshal(clientData, &sinfo)
		if err != nil {
			t.Errorf("error on parsing client data: %v", err)
		}
		if sinfo.ChanName != id0 {
			t.Errorf("unexpected client id: %s", sinfo.ChanName)
		}
		var rsp []byte
		select {
		case ch1 <- must(json.Marshal(&PeerInfo{PeerAddrs: []AddrPair{{sinfo.PriAddr, ra}}, PeerNPlan: nplan})):
			rsp = <-ch2
		case rsp = <-ch1:
			ch2 <- must(json.Marshal(&PeerInfo{PeerAddrs: []AddrPair{{sinfo.PriAddr, rb}}, PeerNPlan: nplan}))
		}
		w.WriteHeader(http.StatusOK)
		err = sendPacket(w, rsp)
		if err != nil {
			t.Errorf("error on replying to client: %v", err)
		}
	}))
	defer server.Close()

	chRaddr := make(chan string)
	runClient := func() {
		cInfo, err := ExchangeConnInfo(context.Background(), server.URL, &SelfInfo{ChanName: id0, NPlan: nplan}, 0, false)
		if err != nil {
			t.Errorf("exchange: %v", err)
		}
		chRaddr <- cInfo.PeerAddrs[0].PubAddr
	}
	go runClient()
	go runClient()
	rx, ry := <-chRaddr, <-chRaddr
	if !(rx == ra && ry == rb) && !(rx == rb && ry == ra) {
		t.Errorf("PeerInfo.peerRaddr from exchange not matched: expect: {%s,%s}, got: {%s,%s}", ra, rb, rx, ry)
	}
}

func TestExchangeConnInfoError(t *testing.T) {
	defaultLogger = &testLogger{t}
	_, err := ExchangeConnInfo(context.Background(), "http://localhost:40404", &SelfInfo{ChanName: "test-exchange-err", NPlan: 1}, 0, false)
	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr.Op != "dial" {
		t.Fatalf("exchangeConnInfo did not return a dial error on dial failure: %v", err)
	}
}

type testLogger struct{ t *testing.T }

func (l *testLogger) Infof(format string, a ...any)  { l.t.Logf("pnet info: "+format, a...) }
func (l *testLogger) Debugf(format string, a ...any) { l.t.Logf("pnet debug: "+format, a...) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
