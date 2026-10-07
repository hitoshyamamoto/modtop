// Command simserver runs the Modbus TCP simulator with a sample map, for
// trying modtop by hand. It is a development tool, not part of modtop.
//
//	go run ./internal/sim/cmd/simserver -addr 127.0.0.1:1502
//	modtop 127.0.0.1:1502 -r 40001-40020
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hitoshyamamoto/modtop/internal/codec"
	"github.com/hitoshyamamoto/modtop/internal/sim"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:1502", "listen address")
	unit := flag.Uint("unit", 1, "unit ID to answer")
	flag.Parse()

	dev := sim.NewDevice(byte(*unit))
	// Holding registers 40001.. (PDU 0..).
	dev.Set(codec.ReadHoldingRegisters, 0,
		0x4248, 0x0000, // 50.0 in ABCD
		0x8000, 0x44A2, // 1300.0 in CDAB
		0x01F4,         // 500
		0xFFF6,         // -10 as int16
		0x0000,         // 40007: missing (exception 02)
		0x0064,         // 100
		0x0001, 0x86A0, // 100000 as uint32 ABCD
		0xFFFF, 0xFFFE, // -2 as int32 ABCD
	)
	dev.SetMissing(codec.ReadHoldingRegisters, 6)
	// Input registers 30001.. mirror the first holding registers.
	dev.Set(codec.ReadInputRegisters, 0, 0x4248, 0x0000, 0x8000, 0x44A2, 0x01F4, 0xFFF6)
	// Coils 00001..00010 and discrete inputs 10001..10010.
	dev.Set(codec.ReadCoils, 0, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0)
	dev.Set(codec.ReadDiscreteInputs, 0, 0, 1, 1, 0, 1, 0, 0, 1, 0, 1)

	srv, err := sim.ListenTCP(*addr, dev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "simserver: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Modbus TCP simulator on %s (unit %d); Ctrl+C to stop\n", srv.Addr(), *unit)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = srv.Close()
}
