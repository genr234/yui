// Command dsu-demo runs a DSU server on 127.0.0.1:26760 and animates slot 0
// (slowly rotating gyro, A button toggling every second) so Dolphin can be
// pointed at it by hand.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"kiosk/controller/internal/pad/dsu"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:26760", "UDP listen address")
	flag.Parse()

	s, err := dsu.NewServer(*addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("DSU server listening on", s.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		if err := s.Run(ctx); err != nil {
			log.Println(err)
		}
	}()

	start := time.Now()
	t := time.NewTicker(16 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			el := now.Sub(start).Seconds()
			s.SetSlot(0, dsu.PadState{
				Cross: int(el)%2 == 0,
				Accel: [3]float32{0, 0, 1},
				Gyro:  [3]float32{0, 30, 0}, // 30 deg/s yaw
			})
		}
	}
}
