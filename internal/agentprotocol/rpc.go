// Package agentprotocol implements bounded stdio clients for pinned agent protocols.
// Process creation and filesystem isolation belong to the external runner, never the web service.
package agentprotocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Packet is a JSON-RPC request, notification or response.
type Packet struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError carries a protocol failure without including the request or credentials.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("agent protocol error %d: %s", e.Code, e.Message)
}

// Peer handles one outstanding client request, with interleaved server events and requests.
type Peer struct {
	writer  io.Writer
	writeMu sync.Mutex
	callMu  sync.Mutex
	next    int
	packets chan Packet
	done    chan struct{}
	readErr error
	Handle  func(context.Context, Packet) (any, error)
}

// NewPeer reads bounded newline-delimited JSON. The caller owns and closes the pipes.
func NewPeer(ctx context.Context, reader io.Reader, writer io.Writer, handler func(context.Context, Packet) (any, error)) *Peer {
	p := &Peer{writer: writer, packets: make(chan Packet, 16), done: make(chan struct{}), Handle: handler}
	go func() {
		defer close(p.done)
		s := bufio.NewScanner(reader)
		s.Buffer(make([]byte, 4096), 4<<20)
		for s.Scan() {
			var packet Packet
			if err := json.Unmarshal(s.Bytes(), &packet); err != nil {
				p.readErr = fmt.Errorf("invalid agent JSON-RPC packet")
				return
			}
			select {
			case p.packets <- packet:
			case <-ctx.Done():
				p.readErr = ctx.Err()
				return
			}
		}
		p.readErr = s.Err()
		if p.readErr == nil {
			p.readErr = io.EOF
		}
	}()
	return p
}

func (p *Peer) send(packet Packet) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	packet.JSONRPC = "2.0"
	return json.NewEncoder(p.writer).Encode(packet)
}

// Notify sends a notification, including cancellation, independently of a pending request.
func (p *Peer) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.send(Packet{Method: method, Params: raw})
}

// Call waits for the matching response while servicing provider updates and permission requests.
func (p *Peer) Call(ctx context.Context, method string, params any, result any) error {
	p.callMu.Lock()
	defer p.callMu.Unlock()
	p.next++
	id, _ := json.Marshal(fmt.Sprintf("rewind-%d", p.next))
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err = p.send(Packet{ID: id, Method: method, Params: raw}); err != nil {
		return err
	}
	for {
		var packet Packet
		select {
		case packet = <-p.packets:
		case <-p.done:
			// Drain queued packets before reporting EOF; a completed turn can precede process exit.
			select {
			case packet = <-p.packets:
			default:
				return p.readErr
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		if packet.Method == "" {
			if string(packet.ID) != string(id) {
				return fmt.Errorf("unexpected agent response identity")
			}
			if packet.Error != nil {
				return packet.Error
			}
			if result == nil {
				return nil
			}
			return json.Unmarshal(packet.Result, result)
		}
		var answer any
		var handleErr error
		if p.Handle != nil {
			answer, handleErr = p.Handle(ctx, packet)
		} else if len(packet.ID) > 0 {
			handleErr = &RPCError{Code: -32601, Message: "unsupported client request"}
		}
		if len(packet.ID) > 0 {
			response := Packet{ID: packet.ID}
			if handleErr != nil {
				response.Error = &RPCError{Code: -32603, Message: "client request failed"}
				if e, ok := handleErr.(*RPCError); ok {
					response.Error = e
				}
			} else {
				response.Result, _ = json.Marshal(answer)
			}
			if err = p.send(response); err != nil {
				return err
			}
		} else if handleErr != nil {
			return handleErr
		}
	}
}

// Wait handles asynchronous updates until the callback reports a terminal outcome.
func (p *Peer) Wait(ctx context.Context, complete func() bool) error {
	p.callMu.Lock()
	defer p.callMu.Unlock()
	for !complete() {
		var packet Packet
		select {
		case packet = <-p.packets:
		case <-p.done:
			select {
			case packet = <-p.packets:
			default:
				return p.readErr
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		if packet.Method == "" {
			return fmt.Errorf("unexpected asynchronous response")
		}
		var answer any
		var err error
		if p.Handle != nil {
			answer, err = p.Handle(ctx, packet)
		} else if len(packet.ID) > 0 {
			err = &RPCError{Code: -32601, Message: "unsupported client request"}
		}
		if len(packet.ID) > 0 {
			response := Packet{ID: packet.ID}
			if err != nil {
				response.Error = &RPCError{Code: -32603, Message: "client request failed"}
			} else {
				response.Result, _ = json.Marshal(answer)
			}
			if err = p.send(response); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}
