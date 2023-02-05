package pnet

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExchangeConnInfoProto(t *testing.T) {
	defaultLogger = &testLogger{t}
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	chRecvOrErr := make(chan readerOrError)
	go func() { chRecvOrErr <- readerOrError{ReadCloser: downR}; close(chRecvOrErr) }()

	laddr, id := "127.0.0.1:30001", "test-exchange-proto"
	cInfo0 := connInfo{laddr, "127.0.0.1:30002", "80.80.80.80:30003"}
	go func() { // mock server protocol
		clientData, err := receivePacket(upR)
		if err != nil {
			t.Errorf("error on receiving client data: %v", err)
		}
		if string(clientData) != vbar(laddr, id) {
			t.Errorf("unexpected client data: %s", string(clientData))
		}
		err = sendPacket(downW, []byte(vbar(cInfo0.peerRaddr, cInfo0.peerLaddr)))
		if err != nil {
			t.Errorf("error on replying to client: %v", err)
		}
	}()

	cInfo, err := exchangeConnInfoProto(context.Background(), upW, chRecvOrErr, laddr, id, nil)
	if err != nil {
		t.Fatalf("exchange proto: %v", err)
	}
	if *cInfo != cInfo0 {
		t.Fatalf("connInfo from exchange proto not matched: expect: %+v, got: %+v", cInfo0, *cInfo)
	}
}

func TestExchangeConnInfo(t *testing.T) {
	defaultLogger = &testLogger{t}
	id0 := "test-exchange"
	ra, rb := "80.80.80.80:30011", "80.80.80.80:30012"
	ch1, ch2 := make(chan string), make(chan string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientData, err := receivePacket(r.Body)
		if err != nil {
			t.Errorf("error on receiving client data: %v", err)
		}
		laddr, id := vsplit(string(clientData))
		if id != id0 {
			t.Errorf("unexpected client id: %s", id)
		}
		var rsp string
		select {
		case ch1 <- vbar(ra, laddr):
			rsp = <-ch2
		case rsp = <-ch1:
			ch2 <- vbar(rb, laddr)
		}
		w.WriteHeader(http.StatusOK)
		err = sendPacket(w, []byte(rsp))
		if err != nil {
			t.Errorf("error on replying to client: %v", err)
		}
	}))
	defer server.Close()

	chRaddr := make(chan string)
	runClient := func() {
		cInfo, err := exchangeConnInfo(context.Background(), server.URL, id0, false)
		if err != nil {
			t.Errorf("exchange: %v", err)
		}
		chRaddr <- cInfo.peerRaddr
	}
	go runClient()
	go runClient()
	rx, ry := <-chRaddr, <-chRaddr
	if !(rx == ra && ry == rb) && !(rx == rb && ry == ra) {
		t.Errorf("connInfo.peerRaddr from exchange not matched: expect: {%s,%s}, got: {%s,%s}", ra, rb, rx, ry)
	}
}

type testLogger struct{ t *testing.T }

func (l *testLogger) Infof(format string, a ...any)  { l.t.Logf("pnet info: "+format, a...) }
func (l *testLogger) Debugf(format string, a ...any) { l.t.Logf("pnet debug: "+format, a...) }
