package stream

import (
	"net"

	aead "github.com/shadowsocks/go-shadowsocks2/shadowaead"
)

func encrypted(conn net.Conn, psk []byte) (net.Conn, error) {
	cipher, err := aead.Chacha20Poly1305(psk)
	conn = aead.NewConn(conn, cipher)
	return conn, err
}
