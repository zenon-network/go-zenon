// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package p2p

import (
	"fmt"
)

const (
	// ErrInvalidMsgCode is the PeerError code for a message code outside
	// the protocol's range.
	ErrInvalidMsgCode = iota
	// ErrInvalidMsg is the PeerError code for a payload that does not
	// decode.
	ErrInvalidMsg
)

var errorToString = map[int]string{
	ErrInvalidMsgCode: "invalid message code",
	ErrInvalidMsg:     "invalid message",
}

// PeerError is a protocol violation by a remote peer, carrying one of
// the codes above and a message. DiscReasonForError maps it to the
// DiscProtocolError disconnect reason.
type PeerError struct {
	code    int
	message string
}

// NewPeerError builds a PeerError for code, appending the formatted
// detail to the code's description when format is non-empty. It panics
// on an unknown code.
func NewPeerError(code int, format string, v ...interface{}) *PeerError {
	desc, ok := errorToString[code]
	if !ok {
		panic("invalid error code")
	}
	err := &PeerError{code, desc}
	if format != "" {
		err.message += ": " + fmt.Sprintf(format, v...)
	}
	return err
}

// Error returns the description and detail.
func (self *PeerError) Error() string {
	return self.message
}

// DiscReason explains why a peer is being disconnected. The reason is
// sent to the remote side in the disconnect message, so both ends
// must agree on the numeric values. They match the devp2p wire
// protocol for 0x00-0x0b, but DiscSubprotocolError is 0x0c here
// rather than devp2p's 0x10.
type DiscReason uint

// The devp2p disconnect reasons.
const (
	DiscRequested DiscReason = iota
	DiscNetworkError
	DiscProtocolError
	DiscUselessPeer
	DiscTooManyPeers
	DiscAlreadyConnected
	DiscIncompatibleVersion
	DiscInvalidIdentity
	DiscQuitting
	DiscUnexpectedIdentity
	DiscSelf
	DiscReadTimeout
	DiscSubprotocolError
)

var discReasonToString = [...]string{
	DiscRequested:           "Disconnect requested",
	DiscNetworkError:        "Network error",
	DiscProtocolError:       "Breach of protocol",
	DiscUselessPeer:         "Useless peer",
	DiscTooManyPeers:        "Too many peers",
	DiscAlreadyConnected:    "Already connected",
	DiscIncompatibleVersion: "Incompatible P2P protocol version",
	DiscInvalidIdentity:     "Invalid node identity",
	DiscQuitting:            "Client quitting",
	DiscUnexpectedIdentity:  "Unexpected identity",
	DiscSelf:                "Connected to self",
	DiscReadTimeout:         "Read timeout",
	DiscSubprotocolError:    "Subprotocol error",
}

// String returns the human-readable reason, or "Unknown Reason(n)" for a
// value outside the declared set, which can arrive from the wire.
func (d DiscReason) String() string {
	// d indexes discReasonToString, so the bound is >= len, not > len. d is also
	// decoded from the wire and is not constrained to the declared reasons.
	if int(d) < 0 || int(d) >= len(discReasonToString) {
		return fmt.Sprintf("Unknown Reason(%d)", d)
	}
	return discReasonToString[d]
}

// Error lets a DiscReason be returned and matched as an error.
func (d DiscReason) Error() string {
	return d.String()
}

// DiscReasonForError maps an error raised while handling a peer to the
// reason sent in the disconnect message: a DiscReason passes through, a
// PeerError becomes DiscProtocolError, anything else
// DiscSubprotocolError.
func DiscReasonForError(err error) DiscReason {
	if reason, ok := err.(DiscReason); ok {
		return reason
	}
	PeerError, ok := err.(*PeerError)
	if ok {
		switch PeerError.code {
		case ErrInvalidMsgCode, ErrInvalidMsg:
			return DiscProtocolError
		default:
			return DiscSubprotocolError
		}
	}
	return DiscSubprotocolError
}
