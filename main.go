// Command jellyfin-to-plex-proxy serves a Plex library over the Jellyfin API.
package main

import (
	"cmp"
	"crypto/tls"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	discoveryAddress = ":7359"
	discoveryProbe   = "who is jellyfinserver?"
	defaultListen    = ":8096"
	defaultListenTLS = ":8920"
	defaultPlexURL   = "http://127.0.0.1:32400"
	defaultUserName  = "jellyfin"
	defaultPassword  = "jellyfin"
	discoveryBuffer  = 1024
)

func requireEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is not set", name)
	}
	return value
}

func main() {
	plexURL, err := url.Parse(cmp.Or(os.Getenv("PLEX_URL"), defaultPlexURL))
	if err != nil {
		log.Fatalf("PLEX_URL: %v", err)
	}
	server, err := NewServer(NewPlex(plexURL, requireEnv("PLEX_TOKEN")), Config{
		UserName: cmp.Or(os.Getenv("JELLYFIN_USERNAME"), defaultUserName),
		Password: cmp.Or(os.Getenv("JELLYFIN_PASSWORD"), defaultPassword),
	})
	if err != nil {
		log.Fatalf("plex: %v", err)
	}
	listen := cmp.Or(os.Getenv("LISTEN"), defaultListen)
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		log.Fatalf("LISTEN: %v", err)
	}
	if conn, err := net.ListenPacket("udp", discoveryAddress); err == nil {
		go server.answerDiscovery(conn, port)
	} else {
		log.Printf("discovery: %v", err)
	}
	if cert, key := os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY"); cert != "" && key != "" {
		secure := &http.Server{Addr: cmp.Or(os.Getenv("LISTEN_TLS"), defaultListenTLS), Handler: server, TLSConfig: tlsConfig(cert, key)}
		go func() {
			log.Printf("serving %q over TLS on %s", server.serverName, secure.Addr)
			log.Fatal(secure.ListenAndServeTLS("", ""))
		}()
	}
	log.Printf("serving %q on %s", server.serverName, listen)
	log.Fatal(http.ListenAndServe(listen, server))
}

// tlsConfig reads the certificate for every connection, so a renewed one
// serves without a restart.
func tlsConfig(certFile, keyFile string) *tls.Config {
	return &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		return &cert, err
	}}
}

// answerDiscovery replies to the UDP broadcast clients send to find servers,
// with the address of whichever interface faces the asker.
func (s *Server) answerDiscovery(conn net.PacketConn, port string) {
	buffer := make([]byte, discoveryBuffer)
	for {
		n, asker, err := conn.ReadFrom(buffer)
		if err != nil {
			log.Printf("discovery: %v", err)
			return
		}
		if !strings.Contains(strings.ToLower(string(buffer[:n])), discoveryProbe) {
			continue
		}
		route, err := net.Dial("udp", asker.String())
		if err != nil {
			continue
		}
		local := route.LocalAddr().(*net.UDPAddr).IP.String()
		route.Close()
		reply, _ := json.Marshal(DiscoveryReply{
			Address: "http://" + net.JoinHostPort(local, port),
			Id:      s.serverID,
			Name:    s.serverName,
		})
		conn.WriteTo(reply, asker)
	}
}
