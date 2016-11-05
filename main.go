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
	"unicode"

	//"github.com/gdamore/tcell/termbox"
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
	err := cmd.Run()
	log.Printf("%q: %v", line, err)
	if err == nil {
		// no error, flip to front
		p.outbuf, p.showbuf = p.showbuf, p.outbuf
	}
	return err
}

func (p *pipeline) renderBuffer(b *bytes.Buffer, skipUpper, skipLower, fromEnd int, fg, bg termbox.Attribute) int {
	cols, rows := termbox.Size()
	lines, n := getBufferLinesToShow(rows-skipUpper-skipLower, cols, fromEnd, b.String())
	for y, row := range lines {
		for x, c := range row {
			termbox.SetCell(x, y+skipUpper, c, fg, bg)
		}
		for x := len(row); x < cols; x++ {
			termbox.SetCell(x, y+skipUpper, ' ', fg, bg)
		}
	}
	return n
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

func getBufferLinesToShow(rows, cols, skipFromEnd int, data string) ([][]rune, int) {
	// turn into []rune so that we account for width correctly. it's also what termbox wants.
	rs := []rune(data)
	linesInReverse := [][]rune{}

	linesToRender := rows + skipFromEnd

	linesToRender--
	for linesToRender >= 0 && len(rs) > 0 {
		// log.Printf("rs=%q", string(rs))
		// find the last '\n', or verify that there isn't one in cols runes
		lastNewline := -1
		for i := len(rs) - 1; i >= 0; i-- {
			if rs[i] == '\n' {
				lastNewline = i
				break
			}
		}

		// break out this line into several max-width pieces
		totalLine := rs[lastNewline+1:]
		// log.Printf("totalLine=%q", string(totalLine))

		// expand tabs
		expandedLine := make([]rune, 0, 2*len(totalLine))
		for _, c := range totalLine {
			if c == '\t' {
				spacesRemaining := TabWidth - len(expandedLine)%TabWidth
				for j := 0; j < spacesRemaining; j++ {
					expandedLine = append(expandedLine, ' ')
				}
			} else {
				if c == '\n' {
					log.Printf("Adding a newline for some reason... lastNewline=%d", lastNewline)
				}
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
				totalLine = totalLine[cols:]
			}
			if len(totalLine) > 0 {
				linePieces = append([][]rune{totalLine}, linePieces...)
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
	if rows+skipFromEnd > len(linesInReverse) {
		skipFromEnd = len(linesInReverse) - rows
		if skipFromEnd < 0 {
			skipFromEnd = 0
		}
	}
	for i := 0; i < len(linesInReverse)-skipFromEnd; i++ {
		lines = append(lines, linesInReverse[len(linesInReverse)-i-1])
	}

	return lines, skipFromEnd
}

func (p *pipeline) render(line string, cursor, fromEnd int, processError bool) (error, int) {
	_, rows := termbox.Size()
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	outFg := termbox.ColorDefault
	if processError {
		outFg = termbox.ColorYellow
	}
	n := p.renderBuffer(p.showbuf, 1, 2, fromEnd, outFg, termbox.ColorDefault)
	lineFg, lineBg := termbox.ColorWhite, termbox.ColorBlack
	if processError {
		p.renderBuffer(p.errbuf, rows-2, 0, 0, termbox.ColorRed, termbox.ColorBlack)
		lineFg = termbox.ColorRed
	} else {
		p.renderBuffer(bytes.NewBufferString("\n\n\n"), rows-2, 0, 0, termbox.ColorRed, termbox.ColorBlack)
	}
	p.renderLine(line, cursor, lineFg, lineBg)
	return termbox.Flush(), n
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
	scrollCh := make(chan int)

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
			io.Copy(os.Stdout, p.showbuf)
		} else {
			os.Exit(1)
		}
	}()

	// draw the screen
	go func() {
		var line string
		var cursor int
		var redraw, processError bool
		var fromEnd int

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
						processError = false
					}
					p.inbuf.Clean()
					redraw = true
				}

				if redraw {
					if err, n := p.render(line, cursor, fromEnd, processError); err != nil {
						log.Fatalf("Could not write to screen: %v", err)
					} else {
						fromEnd = n
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
			case delta := <-scrollCh:
				fromEnd += delta
				if fromEnd < 0 {
					fromEnd = 0
				}
			}
		}
	}()

	// process input
	lineBuffer := ""
	cursor := 0
	ebytes := make([]byte, 16)
loop:
	for {
		re := termbox.PollRawEvent(ebytes)
		log.Printf("re: %#v", re)
		log.Printf("data: %v %s", ebytes, string(ebytes[1:re.N]))

		// If you want to make pipeline cross-platform, here's where you
		// tell it how to recognize special key chords.
		var (
			escEscape         = ""
			escCtrlLeftArrow  = "[1;5D"
			escCtrlRightArrow = "[1;5C"
		)

		e := termbox.ParseEvent(ebytes)
		log.Printf("e: %#v", e)

		if e.Key == termbox.KeyEsc {
			escKey := ebytes[1:re.N]
			switch string(escKey) {
			case escEscape:
				break loop
			case escCtrlLeftArrow:
				if cursor == 0 {
					continue
				}
				i := cursor - 1
				for i > 0 && unicode.IsSpace(rune(lineBuffer[i])) {
					i--
				}
				for i > 0 && !unicode.IsSpace(rune(lineBuffer[i])) {
					i--
				}
				cursor = i
			case escCtrlRightArrow:
				if cursor == len(lineBuffer) {
					continue
				}
				i := cursor
				for i < len(lineBuffer) && unicode.IsSpace(rune(lineBuffer[i])) {
					i++
				}
				for i < len(lineBuffer) && !unicode.IsSpace(rune(lineBuffer[i])) {
					i++
				}
				cursor = i
			}
		}

		switch e.Key {
		case termbox.KeyEnter:
			gracefulExit = true
			break loop
		case termbox.KeyCtrlC:
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
		case termbox.KeyArrowDown:
			scrollCh <- -1
		case termbox.KeyArrowUp:
			scrollCh <- 1
		case termbox.KeyPgdn:
			_, rows := termbox.Size()
			scrollCh <- -rows + 4
		case termbox.KeyPgup:
			_, rows := termbox.Size()
			scrollCh <- rows - 4
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
