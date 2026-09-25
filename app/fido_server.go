package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bulwarkid/bulwark-passkey/app/mac"
	virtual_fido "github.com/bulwarkid/virtual-fido"
)

func startFIDOServer(client *FIDOClient) {
	logFile, err := os.OpenFile(configFilePath("device.log"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	checkErr(err, "Could not open device log")
	defer logFile.Close()
	virtual_fido.SetLogOutput(logFile)

	installVirtualUSBDriverIfNecessary()
	go attachUSBIPServer()
	startFIDODevice(client)
}

func installVirtualUSBDriverIfNecessary() {
	if runtime.GOOS == "darwin" {
		mac.InstallVirtualUSBDriver()
	}
}

// usbipAttaching keeps overlapping attach attempts, and the authorization
// prompts that come with them, from stacking up.
var usbipAttaching atomic.Bool

func attachUSBIPServer() {
	attachUSBIPDevice(250 * time.Millisecond)
}

// attachUSBIPDevice plugs the virtual authenticator into the host after the
// given delay. It is called at startup and again whenever the host detaches
// the device, so that a detach does not require restarting the app.
func attachUSBIPDevice(delay time.Duration) {
	if !usbipAttaching.CompareAndSwap(false, true) {
		debugf("Skipping USB/IP attach, one is already pending")
		return
	}
	defer usbipAttaching.Store(false)
	time.Sleep(delay)
	if runtime.GOOS == "windows" {
		attachUSBIPWindows()
	} else if runtime.GOOS == "linux" {
		attachUSBIPLinux()
	} else if runtime.GOOS == "darwin" {
		// No USB/IP server on Mac; it uses a virtual USB device driver instead
	} else {
		debugf("Could not find USBIP command for OS %s\n", runtime.GOOS)
		return
	}
}

func runCommand(commandList []string) error {
	debugf("%s", strings.Join(commandList, " "))
	prog := exec.Command(commandList[0], commandList[1:]...)
	prog.Stdin = os.Stdin
	prog.Stdout = os.Stdout
	prog.Stderr = os.Stderr
	err := prog.Run()
	if err != nil {
		debugf("Error: %s\n", err)
	}
	return err
}

// runCommandQuietly runs a command without a terminal and without reporting
// the failure, for probing whether a privileged command can run unattended.
func runCommandQuietly(commandList []string) error {
	debugf("%s", strings.Join(commandList, " "))
	prog := exec.Command(commandList[0], commandList[1:]...)
	output, err := prog.CombinedOutput()
	if err != nil {
		debugf("%s failed: %v: %s", commandList[0], err, strings.TrimSpace(string(output)))
	}
	return err
}

const usbipRemoteHost = "127.0.0.1"
const usbipRemoteBusID = "2-2"

func attachUSBIPLinux() {
	// Loading the driver is best effort: it can be built into the kernel,
	// already loaded, or loadable only by an administrator. Asking for it is
	// only worth a possible authorization prompt when it is really missing.
	if !vhciDriverLoaded() {
		if modprobe := findProgram("modprobe", "/usr/sbin/modprobe", "/sbin/modprobe"); modprobe != "" {
			runPrivileged(modprobe, "vhci-hcd")
		} else {
			debugf("Could not find modprobe, attaching without loading vhci-hcd")
		}
	}
	usbip := findProgram("usbip", "/usr/local/bin/usbip", "/usr/sbin/usbip", "/usr/bin/usbip")
	if usbip == "" {
		errorf("Could not find the usbip command, so the authenticator cannot be attached")
		return
	}
	if err := runPrivileged(usbip, "attach", "-r", usbipRemoteHost, "-b", usbipRemoteBusID); err != nil {
		errorf("Could not attach the authenticator: %v", err)
	}
}

// The driver creates this platform device when it is available, and that is
// also what `usbip attach` writes to. /proc/modules is only a fallback, since
// it is not always readable and does not list built-in drivers.
const vhciPlatformDevice = "/sys/devices/platform/vhci_hcd.0"
const vhciModuleName = "vhci_hcd"
const procModules = "/proc/modules"

// vhciDriverLoaded reports whether the USB/IP virtual host controller is
// usable, either as a loaded module or built into the kernel.
func vhciDriverLoaded() bool {
	return driverLoaded(vhciPlatformDevice, procModules, vhciModuleName)
}

func driverLoaded(platformDevice string, modulesFile string, moduleName string) bool {
	if _, err := os.Stat(platformDevice); err == nil {
		return true
	}
	modules, err := os.ReadFile(modulesFile)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(modules), "\n") {
		if name, _, found := strings.Cut(line, " "); found && name == moduleName {
			return true
		}
	}
	return false
}

// runPrivileged runs one program as root, preferring the ways that do not ask
// for a password: nothing at all when the app already runs as root, then
// `sudo -n`, which stays silent unless a NOPASSWD rule covers the command, and
// finally pkexec, which asks through the desktop's polkit agent.
//
// The program is always executed directly, never through a shell, so that
// sudoers, polkit and AppArmor rules can match the exact program and
// arguments instead of having to allow a shell to run as root.
func runPrivileged(program string, args ...string) error {
	command := append([]string{program}, args...)
	if os.Geteuid() == 0 {
		return runCommand(command)
	}
	if sudo := findProgram("sudo", "/usr/bin/sudo", "/bin/sudo"); sudo != "" {
		if err := runCommandQuietly(append([]string{sudo, "-n"}, command...)); err == nil {
			return nil
		}
		debugf("No passwordless sudo rule for %s, asking for authorization instead", program)
	}
	pkexec := findProgram("pkexec", "/usr/bin/pkexec", "/bin/pkexec")
	if pkexec == "" {
		return fmt.Errorf("neither sudo nor pkexec can run %s as root", program)
	}
	return runCommand(append([]string{pkexec}, command...))
}

// findProgram resolves a program to an absolute path, which is what sudoers
// rules, polkit rules and AppArmor profiles match on. PATH usually misses the
// administrative directories for a desktop session, hence the fallbacks.
func findProgram(name string, fallbacks ...string) string {
	if path, err := exec.LookPath(name); err == nil {
		if absolute, err := filepath.Abs(path); err == nil {
			return absolute
		}
		return path
	}
	for _, fallback := range fallbacks {
		if info, err := os.Stat(fallback); err == nil && !info.IsDir() {
			return fallback
		}
	}
	return ""
}

func attachUSBIPWindows() {
	runCommand([]string{"./usbip/usbip.exe", "install", "-u"})
	runCommand([]string{"./usbip/usbip.exe", "attach", "-r", usbipRemoteHost, "-b", usbipRemoteBusID})
}
