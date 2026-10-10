//go:build darwin

package router

/*
#include <launch.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"net"
	"os"
	"unsafe"
)

// launchd opens privileged loopback sockets and hands them to the unprivileged
// Loca process. The router never runs as root.
func ActivatedListener(name string) (net.Listener, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	var fds *C.int
	var count C.size_t
	result := C.launch_activate_socket(cname, &fds, &count)
	if result != 0 {
		return nil, fmt.Errorf("launchd socket %s unavailable (%d)", name, int(result))
	}
	defer C.free(unsafe.Pointer(fds))
	if count != 1 {
		return nil, fmt.Errorf("expected one launchd socket for %s, got %d", name, int(count))
	}
	file := os.NewFile(uintptr(*fds), name)
	defer file.Close()
	return net.FileListener(file)
}
