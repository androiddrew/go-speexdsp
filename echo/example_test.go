package echo_test

import (
	"fmt"
	"log"

	"github.com/androiddrew/go-speexdsp/echo"
)

func Example() {
	// 16 ms frames at 16 kHz, modeling 250 ms of echo path.
	c, err := echo.New(256, 4000, 16000)
	if err != nil {
		log.Fatal(err)
	}
	mic := make([]float32, c.FrameSize())    // what the microphone heard
	played := make([]float32, c.FrameSize()) // what the speaker played meanwhile
	out := make([]float32, c.FrameSize())
	if err := c.Cancel(mic, played, out); err != nil {
		log.Fatal(err)
	}
	fmt.Println(c.FrameSize(), c.FilterLength())
	// Output: 256 4096
}
