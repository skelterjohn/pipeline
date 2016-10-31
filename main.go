package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	logpkg "log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nsf/termbox-go"
)

var log *logpkg.Logger

var (
	logFile    = flag.String("log", "", "File for log writing.")
	shell      = flag.String("shell", "bash", "Shell to use for pipeline evaluation.")
	buffersize = flag.Int("buffersize", 16384, "Maximum size of input buffer.")
)

type buffer struct {
	sync.Mutex
	dirty      bool
	buffer     bytes.Buffer
	buffersize int
}

func (b *buffer) Write(data []byte) (int, error) {
	b.Lock()
	defer b.Unlock()

	overage := b.buffersize - (b.buffer.Len() + len(data))
	if overage > 0 {
		b.buffer.Next(overage)
	}

	n, err := b.buffer.Write(data)
	b.dirty = b.dirty || n != 0
	return n, err
}

func (b *buffer) String() string {
	return b.buffer.String()
}

func (b *buffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *buffer) Dirty() bool {
	b.Lock()
	defer b.Unlock()
	return b.dirty
}

func (b *buffer) Clean() {
	b.Lock()
	defer b.Unlock()
	b.dirty = false
}

type pipeline struct {
	inbuf    *buffer
	outbuf   *bytes.Buffer
	showbuf  *bytes.Buffer
	errbuf   *bytes.Buffer
	lastLine string
}

func (p *pipeline) processPipeline(line string) error {
	p.lastLine = line
	p.outbuf.Truncate(0)
	p.errbuf.Truncate(0)
	if line == "" {
		_, err := fmt.Fprint(p.outbuf, p.inbuf.String())
		p.outbuf, p.showbuf = p.showbuf, p.outbuf
		return err
	}
	cmd := exec.Command(*shell, "-c", line)
	cmd.Stdout = p.outbuf
	cmd.Stderr = p.errbuf
	cmd.Stdin = strings.NewReader(p.inbuf.String())
	log.Printf("pipeline input: %q", p.inbuf.String())
	err := cmd.Run()
	log.Printf("%q: %v", line, err)
	if err == nil {
		// no error, flip to front
		p.outbuf, p.showbuf = p.showbuf, p.outbuf
	}
	return err
}

func (p *pipeline) renderBuffer(b *bytes.Buffer, skipUpper, skipLower int, fg, bg termbox.Attribute) {
	cols, _ := termbox.Size()
	data := b.String()
	for row := skipUpper; row <= cols-skipLower; {
		if len(data) == 0 {
			break
		}
		if data[0] == '\n' {
			row++
			data = data[1:]
			continue
		}
		newlineIndex := strings.Index(data, "\n")
		if newlineIndex == -1 {
			newlineIndex = len(data)
		}
		if newlineIndex > cols {
			newlineIndex = cols
		}
		line := data[:newlineIndex]
		taken := 0
		if len(line) != 0 {
			for i, c := range line {
				if i >= cols {
					break
				}
				taken += utf8.RuneLen(c)
				termbox.SetCell(i, row, c, fg, bg)
			}
			data = data[taken:]
		}
	}
}

func (p *pipeline) renderLine(line string, cursor int, fg, bg termbox.Attribute) {
	termbox.SetCell(0, 0, '|', fg, bg)
	termbox.SetCell(1, 0, ' ', fg, bg)
	end := 2
	for i, c := range line {
		termbox.SetCell(i+2, 0, c, fg, bg)
		end++
	}
	cols, _ := termbox.Size()
	for i := end; i < cols; i++ {
		termbox.SetCell(i, 0, ' ', fg, bg)
	}
	termbox.SetCursor(2+cursor, 0)
}

func (p *pipeline) render(line string, cursor int, processError bool) error {
	_, rows := termbox.Size()
	if err := termbox.Clear(termbox.ColorDefault, termbox.ColorDefault); err != nil {
		return err
	}
	outFg := termbox.ColorDefault
	if processError {
		outFg = termbox.ColorYellow
	}
	p.renderBuffer(p.showbuf, 1, 2, outFg, termbox.ColorDefault)
	lineFg, lineBg := termbox.ColorWhite, termbox.ColorBlack
	if processError {
		p.renderBuffer(p.errbuf, rows-2, 0, termbox.ColorRed, termbox.ColorBlack)
		lineFg = termbox.ColorRed
	}
	p.renderLine(line, cursor, lineFg, lineBg)
	return termbox.Flush()
}

func main() {
	flag.Parse()

	if *logFile != "" {
		fout, err := os.Create(*logFile)
		if err != nil {
			logpkg.Fatalf("Could not open log file: %v", err)
		}
		log = logpkg.New(fout, "", 0)
	} else {
		log = logpkg.New(ioutil.Discard, "", 0)
	}

	if err := termbox.Init(); err != nil {
		log.Fatalf("termbox.Init(): %v", err)
	}

	quit := make(chan struct{})
	lineCh := make(chan string)
	cursorCh := make(chan int)
	redrawCh := make(chan bool)
	errorCh := make(chan bool)

	p := pipeline{
		inbuf:   &buffer{buffersize: *buffersize},
		outbuf:  &bytes.Buffer{},
		showbuf: &bytes.Buffer{},
		errbuf:  &bytes.Buffer{},
	}
	go func() {
		io.Copy(p.inbuf, os.Stdin)
		log.Print("Done with stdin")
	}()

	var gracefulExit bool
	defer func() {
		termbox.Sync()
		termbox.Close()
		if gracefulExit && !<-errorCh {
			io.Copy(os.Stdout, p.outbuf)
		} else {
			os.Exit(1)
		}
	}()

	// draw the screen
	go func() {
		var line string
		var cursor int
		var redraw, processError bool

		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if line != p.lastLine || p.inbuf.Dirty() {
					if err := p.processPipeline(line); err != nil {
						log.Printf("pipeline error: %v", err)
						processError = true
					} else {
						log.Printf("outbuf: %q", p.outbuf.String())
						processError = false
					}
					p.inbuf.Clean()
					redraw = true
				}

				if redraw {
					if err := p.render(line, cursor, processError); err != nil {
						log.Fatalf("Could not write to screen: %v", err)
					}
					redraw = false
				}
			case line = <-lineCh:
			case cursor = <-cursorCh:
				redraw = true
			case redraw = <-redrawCh:
			case <-quit:
				log.Print("Quitting")
				errorCh <- processError
				return
			}
		}
	}()

	// process input
	lineBuffer := ""
	cursor := 0
loop:
	for {
		e := termbox.PollEvent()
		switch e.Key {
		case termbox.KeyEnter:
			gracefulExit = true
			break loop
		case termbox.KeyEsc, termbox.KeyCtrlC:
			break loop
		case termbox.KeySpace:
			lineBuffer = lineBuffer[:cursor] + string(' ') + lineBuffer[cursor:]
			cursor++
		case termbox.KeyBackspace, termbox.KeyBackspace2:
			if cursor != 0 {
				lineBuffer = lineBuffer[:cursor-1] + lineBuffer[cursor:]
				cursor--
			}
		case termbox.KeyArrowLeft:
			if cursor > 0 {
				cursor--
			}
		case termbox.KeyArrowRight:
			if cursor < len(lineBuffer) {
				cursor++
			}
		case 0:
			lineBuffer = lineBuffer[:cursor] + string(e.Ch) + lineBuffer[cursor:]
			cursor++
		}
		if e.Width != 0 || e.Height != 0 {
			redrawCh <- true
		}
		lineCh <- lineBuffer
		cursorCh <- cursor
		log.Printf("%#v", e)
	}
	close(quit)
}
