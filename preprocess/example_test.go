package preprocess_test

import (
	"fmt"
	"log"

	"github.com/androiddrew/go-speexdsp/echo"
	"github.com/androiddrew/go-speexdsp/preprocess"
)

// Cancel the echo, then let the preprocessor suppress what is left of it and
// the background noise, with wonky-talkie's settings for 16 kHz voice.
func Example() {
	c, err := echo.New(256, 4000, 16000)
	if err != nil {
		log.Fatal(err)
	}
	p, err := preprocess.New(preprocess.Config{
		FrameSize:            256,
		SampleRate:           16000,
		NoiseSuppressDB:      -10,
		EchoSuppressDB:       -40,
		EchoSuppressActiveDB: -15,
		Echo:                 c,
	})
	if err != nil {
		log.Fatal(err)
	}
	mic := make([]float32, 256)    // what the microphone heard
	played := make([]float32, 256) // what the speaker played meanwhile
	out := make([]float32, 256)
	if err := c.Cancel(mic, played, out); err != nil {
		log.Fatal(err)
	}
	if err := p.Run(out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("speech probability %.2f\n", p.SpeechProbability())
	// Output: speech probability 0.10
}
