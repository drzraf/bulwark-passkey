//go:build linux || windows

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bulwarkid/virtual-fido/ctap"
	"github.com/bulwarkid/virtual-fido/ctap_hid"
	"github.com/bulwarkid/virtual-fido/u2f"
	"github.com/bulwarkid/virtual-fido/usb"
	"github.com/bulwarkid/virtual-fido/usbip"
)

// USB/IP transport for the virtual authenticator.
//
// This replaces virtual_fido.Start()'s own USB/IP server. That implementation
// turns every socket error into a panic, including the EOF/EPIPE that the
// kernel produces when it resets the connection (a port reset or detach, which
// is what happens when an authentication request is canceled and retried).
// Some of those writes happen on the library's own goroutines, where a panic
// cannot be recovered and takes the whole app down. Here the device is driven
// through virtual-fido's exported device API instead, and a dead connection
// simply ends the session so the next `usbip attach` can be served.

const usbipAddress = ":3240"
const usbipVersion uint16 = 0x0111

// USB/IP control commands, exchanged before a device is imported.
const (
	usbipOpReqDevlist uint16 = 0x8005
	usbipOpRepDevlist uint16 = 0x0005
	usbipOpReqImport  uint16 = 0x8003
	usbipOpRepImport  uint16 = 0x0003
)

const usbipImportErrorStatus uint32 = 1

// USB/IP commands, exchanged once a device has been imported.
const (
	usbipCmdSubmit uint32 = 0x1
	usbipCmdUnlink uint32 = 0x2
	usbipRetSubmit uint32 = 0x3
	usbipRetUnlink uint32 = 0x4
)

const (
	usbipDirOut uint32 = 0x0
	usbipDirIn  uint32 = 0x1
)

const usbipBusIDLength = 32

type usbipControlHeader struct {
	Version uint16
	Command uint16
	Status  uint32
}

type usbipMessageHeader struct {
	Command        uint32
	SequenceNumber uint32
	DeviceID       uint32
	Direction      uint32
	Endpoint       uint32
}

type usbipCommandSubmitBody struct {
	TransferFlags        uint32
	TransferBufferLength uint32
	StartFrame           uint32
	NumberOfPackets      uint32
	Interval             uint32
	SetupBytes           [8]byte
}

type usbipReturnSubmitBody struct {
	Status          uint32
	ActualLength    uint32
	StartFrame      uint32
	NumberOfPackets uint32
	ErrorCount      uint32
	Padding         uint64
}

type usbipCommandUnlinkBody struct {
	UnlinkSequenceNumber uint32
	Padding              [24]byte
}

type usbipReturnUnlinkBody struct {
	Status  int32
	Padding [24]byte
}

// startFIDODevice serves the authenticator until the listener fails, handling
// one attached host at a time.
func startFIDODevice(client *FIDOClient) {
	listener, err := net.Listen("tcp", usbipAddress)
	if err != nil {
		// Usually a second copy of the app already holds the port. The vault
		// stays usable, so report it instead of taking the app down.
		errorf("Could not listen for USB/IP connections: %v", err)
		return
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			errorf("Could not accept USB/IP connection: %v", err)
			// Back off so a persistent accept failure cannot spin the CPU.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if !strings.HasPrefix(conn.RemoteAddr().String(), "127.0.0.1") {
			errorf("Rejected USB/IP connection from non-local address %s", conn.RemoteAddr().String())
			conn.Close()
			continue
		}
		// Each attach gets a fresh device, the same way re-plugging a physical
		// key resets it. It also keeps a dropped session's late responses from
		// leaking into the next one.
		serveUSBIPConnection(newFIDODevice(client), conn)
	}
}

func newFIDODevice(client *FIDOClient) usbip.USBIPDevice {
	ctapServer := ctap.NewCTAPServer(client)
	u2fServer := u2f.NewU2FServer(client)
	hidServer := ctap_hid.NewCTAPHIDServer(ctapServer, u2fServer)
	return usb.NewUSBDevice(&safeDeviceDelegate{delegate: hidServer})
}

// safeDeviceDelegate stops malformed CTAP traffic from killing the app.
// virtual-fido panics on unsupported commands, and the USB device dispatches
// messages on its own goroutine, where such a panic is otherwise fatal. The
// host sees a request that never gets answered instead.
type safeDeviceDelegate struct {
	delegate usb.USBDeviceDelegate
}

func (delegate *safeDeviceDelegate) HandleMessage(transferBuffer []byte) {
	defer func() {
		if err := recover(); err != nil {
			errorf("Virtual FIDO message error: %v", err)
		}
	}()
	delegate.delegate.HandleMessage(transferBuffer)
}

func (delegate *safeDeviceDelegate) SetResponseHandler(handler func(response []byte)) {
	delegate.delegate.SetResponseHandler(handler)
}

// usbipConnection serializes writes to the host and tolerates the host going
// away: once the connection has failed, further responses are dropped instead
// of erroring out on a dead socket.
type usbipConnection struct {
	conn      net.Conn
	writeLock sync.Mutex
	closed    atomic.Bool
}

func (conn *usbipConnection) write(data []byte) {
	conn.writeLock.Lock()
	defer conn.writeLock.Unlock()
	if conn.closed.Load() {
		return
	}
	if _, err := conn.conn.Write(data); err != nil {
		debugf("USB/IP connection lost while responding: %v", err)
		conn.close()
	}
}

// close ends the session. Closing the socket also unblocks the read loop, so
// the server returns to accepting connections when a response fails.
func (conn *usbipConnection) close() {
	if conn.closed.Swap(true) {
		return
	}
	conn.conn.Close()
}

func (conn *usbipConnection) read(data interface{}) error {
	if conn.closed.Load() {
		return net.ErrClosed
	}
	err := binary.Read(conn.conn, binary.BigEndian, data)
	if err != nil {
		conn.close()
	}
	return err
}

func (conn *usbipConnection) readBytes(length int) ([]byte, error) {
	data := make([]byte, length)
	if conn.closed.Load() {
		return nil, net.ErrClosed
	}
	if _, err := io.ReadFull(conn.conn, data); err != nil {
		conn.close()
		return nil, err
	}
	return data, nil
}

func serveUSBIPConnection(device usbip.USBIPDevice, netConn net.Conn) {
	conn := &usbipConnection{conn: netConn}
	defer conn.close()
	for {
		var header usbipControlHeader
		if err := conn.read(&header); err != nil {
			logUSBIPDisconnect("Control connection ended", err)
			return
		}
		switch header.Command {
		case usbipOpReqDevlist:
			conn.write(usbipDeviceListReply(device))
		case usbipOpReqImport:
			busID, err := conn.readBytes(usbipBusIDLength)
			if err != nil {
				logUSBIPDisconnect("Could not read bus ID", err)
				return
			}
			if usbipString(busID) != device.BusID() {
				errorf("Host asked for unknown USB/IP device %q", usbipString(busID))
				conn.write(toUSBIPBytes(usbipControlHeader{
					Version: usbipVersion,
					Command: usbipOpRepImport,
					Status:  usbipImportErrorStatus,
				}))
				continue
			}
			conn.write(usbipImportReply(device))
			handleUSBIPCommands(device, conn)
			return
		default:
			errorf("Unknown USB/IP control command 0x%x", header.Command)
			return
		}
	}
}

func handleUSBIPCommands(device usbip.USBIPDevice, conn *usbipConnection) {
	for {
		var header usbipMessageHeader
		if err := conn.read(&header); err != nil {
			logUSBIPDisconnect("Device detached", err)
			return
		}
		switch header.Command {
		case usbipCmdSubmit:
			if !handleUSBIPSubmit(device, conn, header) {
				return
			}
		case usbipCmdUnlink:
			if !handleUSBIPUnlink(device, conn, header) {
				return
			}
		default:
			errorf("Unsupported USB/IP command %d", header.Command)
			conn.close()
			return
		}
	}
}

func handleUSBIPSubmit(device usbip.USBIPDevice, conn *usbipConnection, header usbipMessageHeader) bool {
	var body usbipCommandSubmitBody
	if err := conn.read(&body); err != nil {
		logUSBIPDisconnect("Could not read submitted command", err)
		return false
	}
	transferBuffer := make([]byte, body.TransferBufferLength)
	if header.Direction == usbipDirOut && body.TransferBufferLength > 0 {
		data, err := conn.readBytes(int(body.TransferBufferLength))
		if err != nil {
			logUSBIPDisconnect("Could not read transfer buffer", err)
			return false
		}
		copy(transferBuffer, data)
	}
	// The device answers interrupt requests later, from its own goroutines, so
	// the reply goes out through a callback that tolerates a dead connection.
	onFinish := func(response []byte) {
		if response != nil {
			copy(transferBuffer, response)
		}
		reply := new(bytes.Buffer)
		binary.Write(reply, binary.BigEndian, usbipMessageHeader{
			Command:        usbipRetSubmit,
			SequenceNumber: header.SequenceNumber,
		})
		binary.Write(reply, binary.BigEndian, usbipReturnSubmitBody{
			ActualLength: uint32(len(transferBuffer)),
		})
		if header.Direction == usbipDirIn {
			reply.Write(transferBuffer)
		}
		conn.write(reply.Bytes())
	}
	return handleDeviceMessage(device, conn, header, body, transferBuffer, onFinish)
}

// handleDeviceMessage keeps virtual-fido's panics (unsupported descriptors,
// invalid endpoints, malformed CTAP commands) from killing the app; the
// session is dropped instead, and the host can attach again.
func handleDeviceMessage(
	device usbip.USBIPDevice,
	conn *usbipConnection,
	header usbipMessageHeader,
	body usbipCommandSubmitBody,
	transferBuffer []byte,
	onFinish func(response []byte)) (handled bool) {
	defer func() {
		if err := recover(); err != nil {
			errorf("Virtual FIDO device error: %v", err)
			conn.close()
			handled = false
		}
	}()
	device.HandleMessage(header.SequenceNumber, onFinish, header.Endpoint, body.SetupBytes[:], transferBuffer)
	return true
}

func handleUSBIPUnlink(device usbip.USBIPDevice, conn *usbipConnection, header usbipMessageHeader) bool {
	var body usbipCommandUnlinkBody
	if err := conn.read(&body); err != nil {
		logUSBIPDisconnect("Could not read unlink command", err)
		return false
	}
	status := -int32(syscall.ENOENT)
	if device.RemoveWaitingRequest(body.UnlinkSequenceNumber) {
		status = -int32(syscall.ECONNRESET)
	}
	reply := new(bytes.Buffer)
	binary.Write(reply, binary.BigEndian, usbipMessageHeader{
		Command:        usbipRetUnlink,
		SequenceNumber: header.SequenceNumber,
	})
	binary.Write(reply, binary.BigEndian, usbipReturnUnlinkBody{Status: status})
	conn.write(reply.Bytes())
	return true
}

func usbipDeviceListReply(device usbip.USBIPDevice) []byte {
	reply := new(bytes.Buffer)
	binary.Write(reply, binary.BigEndian, usbipControlHeader{
		Version: usbipVersion,
		Command: usbipOpRepDevlist,
	})
	binary.Write(reply, binary.BigEndian, uint32(1))
	binary.Write(reply, binary.BigEndian, device.DeviceSummary())
	return reply.Bytes()
}

func usbipImportReply(device usbip.USBIPDevice) []byte {
	reply := new(bytes.Buffer)
	binary.Write(reply, binary.BigEndian, usbipControlHeader{
		Version: usbipVersion,
		Command: usbipOpRepImport,
	})
	binary.Write(reply, binary.BigEndian, device.DeviceSummary().Header)
	return reply.Bytes()
}

func toUSBIPBytes(value interface{}) []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, value)
	return buffer.Bytes()
}

// usbipString reads a NUL-padded fixed-length string field.
func usbipString(data []byte) string {
	if end := bytes.IndexByte(data, 0); end >= 0 {
		data = data[:end]
	}
	return string(data)
}

// logUSBIPDisconnect reports why a session ended. A host that detaches cleanly
// closes the socket, so EOF is normal rather than an error.
func logUSBIPDisconnect(message string, err error) {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		debugf("USB/IP: %s", message)
		return
	}
	debugf("USB/IP: %s: %v", message, err)
}
