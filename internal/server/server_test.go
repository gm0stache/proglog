package server

import (
	"context"
	"net"
	"testing"

	api "github.com/justagabriel/proglog/api/v1"
	"github.com/justagabriel/proglog/internal"
	"github.com/justagabriel/proglog/internal/config"
	"github.com/justagabriel/proglog/internal/log"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestServer(t *testing.T) {
	scenarios := map[string]func(t *testing.T, client api.LogClient, config *Config){
		"create/get a message from/to the log succeeds": testCreateGet,
		"consume past log boundary fails":               testGetPastBoundary,
		"create/get a stream succeeds":                  testCreateGetStream,
	}

	for title, scenario := range scenarios {
		t.Run(title, func(t *testing.T) {
			client, config, teardown := setupTest(t, nil)
			defer teardown()
			scenario(t, client, config)
		})
	}
}

func setupTest(t *testing.T, fn func(*Config)) (client api.LogClient, cfg *Config, teardown func()) {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	serverTLSConfig, err := config.SetupTLSConfig(config.TlSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		ServerAddress: l.Addr().String(),
	})
	require.NoError(t, err)
	serverCreds := credentials.NewTLS(serverTLSConfig)

	dir := internal.GetTempDir("server-test")
	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)

	cfg = &Config{
		CommitLog: clog,
	}
	if fn != nil {
		fn(cfg)
	}
	server, err := NewGRPCServer(cfg, grpc.Creds(serverCreds))
	require.NoError(t, err)

	go func() {
		server.Serve(l)
	}()

	clientTLSConfig, err := config.SetupTLSConfig(config.TlSConfig{
		CAFile: config.CAFile,
	})
	require.NoError(t, err)

	clientCreds := credentials.NewTLS(clientTLSConfig)
	cc, err := grpc.Dial(l.Addr().String(), grpc.WithTransportCredentials(clientCreds))
	require.NoError(t, err)

	client = api.NewLogClient(cc)
	return client, cfg, func() {
		server.Stop()
		cc.Close()
		l.Close()
		clog.Remove()
	}
}

func testCreateGet(t *testing.T, client api.LogClient, config *Config) {
	// arrange
	ctx := context.Background()
	want := &api.Record{
		Value: []byte("hello world"),
	}

	createResp, err := client.Create(
		ctx,
		&api.CreateRecordRequest{
			Record: want,
		},
	)
	require.NoError(t, err)

	getReq := &api.GetRecordRequest{
		Offset: createResp.Offset,
	}

	// act
	getResp, err := client.Get(ctx, getReq)

	// assert
	require.NoError(t, err)
	require.Equal(t, want.Value, getResp.Record.Value)
	require.Equal(t, want.Offset, getResp.Record.Offset)
}

func testGetPastBoundary(t *testing.T, client api.LogClient, config *Config) {
	// arrange
	ctx := context.Background()

	createReq := &api.CreateRecordRequest{
		Record: &api.Record{
			Value: []byte("hello world!"),
		},
	}
	createResp, err := client.Create(ctx, createReq)
	require.NoError(t, err)

	getReq := &api.GetRecordRequest{
		Offset: createResp.Offset + 1,
	}

	// act
	getResp, err := client.Get(ctx, getReq)

	// assert
	if getResp != nil {
		t.Fatal("expected no response, since invalid request!")
	}

	got := status.Code(err)
	want := status.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err())
	if got != want {
		t.Fatalf("got err: %v, want: %v", got, want)
	}
}

func testCreateGetStream(t *testing.T, client api.LogClient, config *Config) {
	// arrange
	ctx := context.Background()

	records := []api.Record{
		{
			Value:  []byte("hello world 1!"),
			Offset: 0,
		},
		{
			Value:  []byte("hello world 2!"),
			Offset: 1,
		},
	}

	// act
	stream, err := client.CreateStream(ctx)

	// assert
	require.NoError(t, err)

	for offset, record := range records {
		createReq := &api.CreateRecordRequest{
			Record: &record,
		}
		err = stream.Send(createReq)
		require.NoError(t, err)

		createResp, err := stream.Recv()
		require.NoError(t, err)
		if createResp.Offset != uint64(offset) {
			t.Fatalf("got offset: %d, want: %d", createResp.Offset, offset)
		}
	}

	// act
	getStream, err := client.GetStream(ctx)

	// assert
	require.NoError(t, err)

	for i, record := range records {
		err = getStream.Send(&api.GetRecordRequest{Offset: uint64(i)})
		require.NoError(t, err)
		res, err := getStream.Recv()
		require.NoError(t, err)
		require.Equal(t, res.Record, &api.Record{
			Value:  record.Value,
			Offset: uint64(i),
		})
	}
}

func TestServerRequiresClientTLSCert(t *testing.T) {
	// arrange
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	serverTLSConfig, err := config.SetupTLSConfig(config.TlSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		ServerAddress: l.Addr().String(),
		Server:        true,
	})
	require.NoError(t, err)
	serverCreds := credentials.NewTLS(serverTLSConfig)

	dir := internal.GetTempDir("server-test")
	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)

	cfg := &Config{
		CommitLog: clog,
	}
	server, err := NewGRPCServer(cfg, grpc.Creds(serverCreds))
	require.NoError(t, err)

	go func() {
		server.Serve(l)
	}()

	credsWithoutTLSCert := grpc.WithTransportCredentials(insecure.NewCredentials())

	// act
	clientConnection, err := grpc.Dial(l.Addr().String(), credsWithoutTLSCert)

	// assert
	require.NoError(t, err, "the connection it self should work, only the auth should fail.")
	require.NotEqual(t, clientConnection.GetState(), connectivity.Ready, "should be unable to connect due to missing TLS cert")
}
