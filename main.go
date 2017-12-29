// An SFTP server implementation serving from s3using the golang SFTP package.
package main

import (
  "flag"
  "fmt"
  "io"
  "io/ioutil"
  "log"
  "net"
  "net/http"
  "net/http/pprof"
  "os"

  "github.com/aws/aws-sdk-go/service/s3"
  "github.com/pkg/sftp"
  "golang.org/x/crypto/ssh"
)

// Based on example server code from golang.org/x/crypto/ssh and server_standalone
func main() {

  var (
    readOnly    bool
    debugStderr bool
  )

  flag.BoolVar(&readOnly, "R", false, "read-only server")
  flag.BoolVar(&debugStderr, "e", false, "debug to stderr")
  flag.Parse()

  r := http.NewServeMux()
  // Register pprof handlers
  r.HandleFunc("/debug/pprof/", pprof.Index)
  r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
  r.HandleFunc("/debug/pprof/profile", pprof.Profile)
  r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
  r.HandleFunc("/debug/pprof/trace", pprof.Trace)

  go http.ListenAndServe(":8080", r)

  debugStream := ioutil.Discard
  if debugStderr {
    debugStream = os.Stderr
  }
  // An SSH server is represented by a ServerConfig, which holds
  // certificate details and handles authentication of ServerConns.
  config := &ssh.ServerConfig{
    PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
      // Should use constant-time compare (or better, salt+hash) in
      // a production setting.
      client := s3Client(c.User(), string(pass))
      input := &s3.ListBucketsInput{}

      _, err := client.ListBuckets(input)

      if err != nil {
        return nil, fmt.Errorf("Authentication rejected for %q", c.User())
      }

      fmt.Fprintf(debugStream, "Login: %s\n", c.User())

      return &ssh.Permissions{Extensions: map[string]string{"ACCESS_KEY_ID": c.User(), "SECRET_KEY_ID": string(pass)}}, nil
    },
  }

  privateBytes, err := ioutil.ReadFile("id_rsa")
  if err != nil {
    log.Fatal("Failed to load private key", err)
  }

  private, err := ssh.ParsePrivateKey(privateBytes)
  if err != nil {
    log.Fatal("Failed to parse private key", err)
  }

  config.AddHostKey(private)

  // Once a ServerConfig has been configured, connections can be
  // accepted.
  listener, err := net.Listen("tcp", "0.0.0.0:22")

  if err != nil {
    log.Fatal("failed to listen for connection", err)
  }

  fmt.Printf("Listening on %v\n", listener.Addr())

  for {
    nConn, err := listener.Accept()

    if err != nil {
      log.Println("failed to accept incoming connection", err)
    } else {
      go handleConnection(nConn, config, debugStream)
    }
  }
  // Before use, a handshake must be performed on the incoming net.Conn.
}

func handleConnection(nConn net.Conn, config *ssh.ServerConfig, debugStream io.Writer) {
  sconn, chans, reqs, err := ssh.NewServerConn(nConn, config)

  if err != nil {
    // log.Println("failed to handshake", err)
    return
  }

  access_key := sconn.Permissions.Extensions["ACCESS_KEY_ID"]
  secret_key := sconn.Permissions.Extensions["SECRET_KEY_ID"]

  log.Println("login detected:", access_key)

  fmt.Fprintf(debugStream, "SSH server established\n")

  // The incoming Request channel must be serviced.
  go ssh.DiscardRequests(reqs)
  // Service the incoming Channel channel.
  for newChannel := range chans {
    // Channels have a type, depending on the application level
    // protocol intended. In the case of an SFTP session, this is "subsystem"
    // with a payload string of "<length=4>sftp"
    fmt.Fprintf(debugStream, "Incoming channel: %s\n", newChannel.ChannelType())
    if newChannel.ChannelType() != "session" {
      newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
      fmt.Fprintf(debugStream, "Unknown channel type: %s\n", newChannel.ChannelType())
      continue
    }
    channel, requests, err := newChannel.Accept()
    if err != nil {
      log.Println("could not accept channel.", err)
      return
    }
    fmt.Fprintf(debugStream, "Channel accepted\n")

    // Sessions have out-of-band requests such as "shell",
    // "pty-req" and "env".  Here we handle only the
    // "subsystem" request.
    go func(in <-chan *ssh.Request) {
      for req := range in {
        fmt.Fprintf(debugStream, "Request: %v\n", req.Type)
        ok := false
        switch req.Type {
        case "subsystem":
          fmt.Fprintf(debugStream, "Subsystem: %s\n", req.Payload[4:])
          if string(req.Payload[4:]) == "sftp" {
            ok = true
          }
        }
        fmt.Fprintf(debugStream, " - accepted: %v\n", ok)
        req.Reply(ok, nil)
      }
    }(requests)

    root := S3Handler(access_key, secret_key)
    server := sftp.NewRequestServer(channel, root)
    if err := server.Serve(); err == io.EOF {
      server.Close()
      log.Print("sftp client exited session.")
    } else if err != nil {
      log.Print("sftp server completed with error:", err)
      server.Close()
    }
  }
}
