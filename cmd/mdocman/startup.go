package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"syscall"
)

// A nil listener and nil error mean another process already owns the port.
func listenWorkspace(port string, out io.Writer, open func(string)) (net.Listener, error) {
	listener, err := net.Listen("tcp", ":"+port)
	if err == nil {
		return listener, nil
	}
	// Windows reports the native Winsock error rather than the POSIX constant.
	if !errors.Is(err, syscall.EADDRINUSE) &&
		!(runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(10048))) {
		return nil, err
	}
	target := "http://localhost:" + port + "/"
	fmt.Fprintf(out, "端口 %s 已有服务，打开 %s\n", port, target)
	open(target)
	return nil, nil
}

func openWorkspaceBrowser(target string) {
	if !hasEmbeddedFrontend() || os.Getenv("MDOC_NO_BROWSER") != "" {
		return
	}
	if err := openBrowser(target); err != nil {
		log.Printf("无法自动打开浏览器，请手动访问 %s：%v", target, err)
	}
}
