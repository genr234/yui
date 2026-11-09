// Command dsu-demo runs a DSU server (default 127.0.0.1:26760) and animates a
// fake pad (slowly rotating gyro; A toggling every second) so an emulator or
// any other DSU client can be pointed at it by hand.
//
// Flags: -addr picks the listen address (use 127.0.0.1:26761 to stand in for
// the system pad), -slots how many slots to animate, -hold a comma-separated
// list of buttons to keep pressed instead of toggling Cross (names as in
// dsu.PadState: Up Down Left Right Cross Circle Square Triangle L1 R1 L2 R2
// L3 R3 Share Options PS Touch), -no-motion to report a still gyro.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"kiosk/controller/internal/pad/dsu"
)

func hold(st *dsu.PadState, names string) error {
	buttons := map[string]*bool{
		"up": &st.Up, "down": &st.Down, "left": &st.Left, "right": &st.Right,
		"cross": &st.Cross, "circle": &st.Circle, "square": &st.Square, "triangle": &st.Triangle,
		"l1": &st.L1, "r1": &st.R1, "l2": &st.L2, "r2": &st.R2, "l3": &st.L3, "r3": &st.R3,
		"share": &st.Share, "options": &st.Options, "ps": &st.PS, "touch": &st.Touch,
	}
	for _, n := range strings.Split(names, ",") {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		b, ok := buttons[n]
		if !ok {
			return fmt.Errorf("unknown button %q", n)
		}
		*b = true
	}
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:26760", "UDP listen address")
	slots := flag.Int("slots", 1, "number of slots (from 0) to animate")
	held := flag.String("hold", "", "comma-separated buttons to hold down (default: toggle Cross every second)")
	noMotion := flag.Bool("no-motion", false, "report no gyro rotation")
	flag.Parse()

	var fixed dsu.PadState
	if err := hold(&fixed, *held); err != nil {
		log.Fatal(err)
	}

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
			st := fixed
			if *held == "" {
				st.Cross = int(el)%2 == 0
			}
			st.Accel = [3]float32{0, 0, 1}
			if !*noMotion {
				st.Gyro = [3]float32{0, 30, 0} // 30 deg/s yaw
			}
			for slot := 0; slot < *slots && slot < 4; slot++ {
				s.SetSlot(slot, st)
			}
		}
	}
}
