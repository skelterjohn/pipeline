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

	"github.com/nsf/termbox-go"
)

const TabWidth = 4

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
	cols, rows := termbox.Size()
	lines := getBufferLinesToShow(rows-skipUpper-skipLower, cols, 0, b.String())
	for y, row := range lines {
		for x, c := range row {
			termbox.SetCell(x, y+skipUpper, c, fg, bg)
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

func getBufferLinesToShow(rows, cols, skipFromEnd int, data string) [][]rune {
	// turn into []rune so that we account for width correctly. it's also what termbox wants.
	rs := []rune(data)
	linesInReverse := [][]rune{}

	linesToRender := rows + skipFromEnd

	linesToRender--
	for linesToRender >= 0 && len(rs) > 0 {
		log.Printf("line %d\n", linesToRender)
		log.Printf("rs=%q", string(rs))
		// find the last '\n', or verify that there isn't one in cols runes
		lastNewline := -1
		for i := len(rs) - 1; i >= 0 && i > len(rs)-cols; i-- {
			if rs[i] == '\n' {
				lastNewline = i
				break
			}
		}

		// break out this line into several max-width pieces
		var totalLine []rune
		if lastNewline == -1 {
			totalLine = rs
		} else {
			totalLine = rs[lastNewline+1:]
		}
		log.Printf("totalLine=%q", string(totalLine))

		// expand tabs
		expandedLine := make([]rune, 0, 2*len(totalLine))
		for _, c := range totalLine {
			if c == '\t' {
				log.Printf("found a tab at %d", len(expandedLine))
				spacesRemaining := TabWidth - len(expandedLine)%TabWidth
				log.Printf("adding %d spaces", spacesRemaining)
				for j := 0; j < spacesRemaining; j++ {
					expandedLine = append(expandedLine, ' ')
				}
			} else {
				expandedLine = append(expandedLine, c)
			}
		}
		totalLine = expandedLine

		linePieces := make([][]rune, 0, 1+len(totalLine)/cols)
		if len(totalLine) == 0 {
			linePieces = [][]rune{{}}
		} else {
			for len(totalLine) > cols {
				linePieces = append([][]rune{totalLine[:cols]}, linePieces...)
				log.Printf("%q added", string(totalLine[:cols]))
				totalLine = totalLine[cols:]
			}
			if len(totalLine) > 0 {
				linePieces = append([][]rune{totalLine}, linePieces...)
				log.Printf("%q added last", string(totalLine))
			}
		}
		if lastNewline != -1 {
			rs = rs[:lastNewline]
		} else {
			rs = rs[:0]
		}

		for _, l := range linePieces {
			linesInReverse = append(linesInReverse, l)
		}
		linesToRender -= len(linePieces)
	}
	lines := [][]rune{}
	for _, l := range linesInReverse {
		log.Printf("r> %s", string(l))
	}
	for i := 0; i < len(linesInReverse)-skipFromEnd; i++ {
		lines = append(lines, linesInReverse[len(linesInReverse)-i-1])
	}

	return lines
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
		if e := recover(); e != nil {
			log.Printf("recover: %v", e)
		}
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
		log.Printf("%#v", e)
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
		case termbox.KeyCtrlA:
			cursor = 0
		case termbox.KeyCtrlK:
			lineBuffer = lineBuffer[:cursor]
		case 0:
			lineBuffer = lineBuffer[:cursor] + string(e.Ch) + lineBuffer[cursor:]
			cursor++
		}
		if e.Width != 0 || e.Height != 0 {
			redrawCh <- true
		}
		lineCh <- lineBuffer
		cursorCh <- cursor
	}
	close(quit)
}
