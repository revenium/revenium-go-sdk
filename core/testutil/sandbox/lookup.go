package sandbox

import (
	"net"
	"strings"
	"time"
)

const (
	streamLengthPrefix = 2
	dnsHeaderLength    = 12
)

// blockedLookup stands in for the connection to a DNS server. The Go
// resolver writes the query to it and gets an error back, so the lookup
// fails without a packet leaving the machine. The resolver frames the query
// as a stream (length-prefixed) because blockedLookup is not a PacketConn.
type blockedLookup struct {
	onQuery func(name string)
}

func (c *blockedLookup) Write(query []byte) (int, error) {
	c.onQuery(questionName(query))
	return 0, errBlocked
}

func (c *blockedLookup) Read([]byte) (int, error)         { return 0, errBlocked }
func (c *blockedLookup) Close() error                     { return nil }
func (c *blockedLookup) LocalAddr() net.Addr              { return blockedAddr{} }
func (c *blockedLookup) RemoteAddr() net.Addr             { return blockedAddr{} }
func (c *blockedLookup) SetDeadline(time.Time) error      { return nil }
func (c *blockedLookup) SetReadDeadline(time.Time) error  { return nil }
func (c *blockedLookup) SetWriteDeadline(time.Time) error { return nil }

type blockedAddr struct{}

func (blockedAddr) Network() string { return "sandbox" }
func (blockedAddr) String() string  { return "sandbox" }

func questionName(query []byte) string {
	offset := streamLengthPrefix + dnsHeaderLength
	var labels []string
	for offset < len(query) {
		length := int(query[offset])
		offset++
		if length == 0 || offset+length > len(query) {
			break
		}
		labels = append(labels, string(query[offset:offset+length]))
		offset += length
	}
	return strings.Join(labels, ".")
}
