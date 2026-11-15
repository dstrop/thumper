//go:build integration

package thumper

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	netrpc "net/rpc"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dstrop/thumper/amqp"
	goridge "github.com/roadrunner-server/goridge/v4/pkg/rpc"
)

// TestPHPPublisher uses the rabbitmq service from docker-compose.yaml.
// It stops only the rabbitmq service in project thumper-confirm-check.
func TestPHPPublisher(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	var dropReplies atomic.Bool
	frameSent := make(chan struct{}, 1)
	go func() {
		for {
			client, acceptErr := proxy.Accept()
			if acceptErr != nil {
				return
			}
			go proxyAMQP(ctx, client, &dropReplies, frameSent)
		}
	}()

	client := amqp.NewClient("amqp://user:password@"+proxy.Addr().String(), time.Millisecond, nil, slog.New(slog.DiscardHandler))
	publisher := client.NewPublisher(amqp.PublisherOptions{ConfirmMode: true, PoolSize: 1})
	t.Cleanup(func() { _ = publisher.Close() })
	server := netrpc.NewServer()
	if err := server.RegisterName("thumper", &rpc{plugin: &Plugin{publisher: publisher}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go server.ServeCodec(goridge.NewCodec(conn))
		}
	}()
	php := func(mode string) *exec.Cmd {
		return exec.CommandContext(ctx, "php", "tests/Integration/client.php", "tcp://"+listener.Addr().String(), mode)
	}
	for _, mode := range []string{"normal", "missing", "normal"} {
		output, runErr := php(mode).CombinedOutput()
		if runErr != nil {
			t.Fatalf("PHP %s: %v\n%s", mode, runErr, output)
		}
		t.Log(string(output))
	}

	// Suppress replies only after normal publishing leaves an open pooled channel.
	// The proxy forwards the whole body before the test stops RabbitMQ.
	dropReplies.Store(true)
	cmd := php("outage")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-frameSent:
	case err := <-done:
		t.Fatalf("PHP ended before sending the message: %v\n%s", err, &output)
	case <-ctx.Done():
		t.Fatal("publish frame did not reach RabbitMQ")
	}
	stop := exec.CommandContext(ctx, "docker", "compose", "-p", "thumper-confirm-check", "stop", "rabbitmq")
	if stopOutput, err := stop.CombinedOutput(); err != nil {
		t.Fatalf("stop test RabbitMQ: %v\n%s", err, stopOutput)
	}
	if err := <-done; err != nil {
		t.Fatalf("PHP outage: %v\n%s", err, &output)
	}
	t.Log(output.String())
}

// proxyAMQP forwards client frames and can suppress broker confirms. EOF still
// reaches the client, so stopping RabbitMQ causes a real connection shutdown.
func proxyAMQP(ctx context.Context, client net.Conn, dropReplies *atomic.Bool, frameSent chan<- struct{}) {
	defer func() { _ = client.Close() }()
	upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", "127.0.0.1:5672")
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()
	go func() {
		defer func() { _ = client.Close() }()
		buf := make([]byte, 64*1024)
		for {
			n, readErr := upstream.Read(buf)
			if n > 0 && !dropReplies.Load() {
				if _, err := io.Copy(client, bytes.NewReader(buf[:n])); err != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	if _, err := io.CopyN(upstream, client, 8); err != nil { // AMQP protocol header
		return
	}
	for {
		var header [7]byte
		if _, err := io.ReadFull(client, header[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(header[3:])
		if size > 16*1024*1024 {
			return
		}
		body := make([]byte, int(size)+1)
		if _, err := io.ReadFull(client, body); err != nil {
			return
		}
		if _, err := io.Copy(upstream, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(body))); err != nil {
			return
		}
		if header[0] == 3 && dropReplies.Load() { // content body frame
			select {
			case frameSent <- struct{}{}:
			default:
			}
		}
	}
}
