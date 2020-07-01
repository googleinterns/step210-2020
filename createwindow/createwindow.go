package createwindow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime"

	"sync"

	"../command"

	"github.com/chromedp/chromedp"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
)

const (
	windowHeight      = 1400
	windowWidth       = 2000
	chromeConnTimeout = 30
)

// For everything that needs to use program list
type Session struct {
	windowList []Quitter
	lock       sync.Mutex
}

// CreateChromeWindow opens a Chrome browser session
func (s *Session) CreateChromeWindow(cmd command.ExternalCommand, ctxCh chan context.Context, cmdErrorHandler func(p *command.ProgramState, err error) error) error {
	programstate, err := command.ExecuteProgram(cmd, cmdErrorHandler)
	if err != nil {
		log.Println(err)
		return err
	}

	ctx, err := establishChromeConnection(programstate, chromeConnTimeout)
	if err != nil {
		log.Println(err)
		return err
	}

	ctxCh <- ctx
	s.appendWindowList(ChromeWindow{programstate})

	return nil
}

func (s *Session) appendWindowList(newWindow Quitter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.windowList = append(s.windowList, newWindow)
}

func (s *Session) getWindowList() []Quitter {
	s.lock.Lock()
	defer s.lock.Unlock()
	programs := make([]Quitter, len(s.windowList))
	copy(programs, s.windowList)
	return programs
}

// Layout has the x , y coordinates of top left corner and width and height of window
type Layout struct {
	x, y uint32
	w, h uint16
}

// Quitters has method quit that closes that window and kills that program
type Quitter interface {
	Quit()
	ToClose() bool
}

type ChromeWindow struct {
	*command.ProgramState
}

type InputWindow struct {
	Wid  xproto.Window
	Conn *xgb.Conn
}

// Quit method to close the Chrome browser sessions
func (p ChromeWindow) Quit() {
	p.Command.Process.Kill()
}

// ToClose method checks whether ChromeWindow needs to be closed
func (p ChromeWindow) ToClose() bool {
	return p.IsRunning()
}

// Quit method to close the input window
func (p *InputWindow) Quit() {
	p.Conn.Close()
}

// ToClose method checks whether InputWindow needs to be closed
func (p *InputWindow) ToClose() bool {
	return true
}

// ForceQuit closes all windows
func (s *Session) ForceQuit() {
	programs := s.getWindowList()

	fmt.Println("starting force quit")

	for _, q := range programs {
		if q.ToClose() == true {
			q.Quit() // will be quitting the other open Chrome Windows
		}
	}
}

// Setup opens all windows and establishes connection with the x server
func Setup(n int, ctxCh chan context.Context) (*xgb.Conn, xproto.Window, error) {
	debuggingport := 9222
	var displayString string

	var session Session
	cmdErrorHandler := func(p *command.ProgramState, err error) error {
		if err != nil {
			fmt.Println("returned error %s, calling force quit", err.Error())
		}
		session.ForceQuit()
		return err
	}

	if runtime.GOOS != "darwin" {
		displayNumber := 1000 + rand.Intn(9999-1000+1)
		displayString := fmt.Sprintf(":%d", displayNumber)
		var xephyrLayout Layout
		xephyrLayout.h, xephyrLayout.w = DefaultXephyrSize()
		if err := session.CreateXephyrWindow(xephyrLayout, n, cmdErrorHandler); err != nil {
			return nil, 0, err
		}
	}

	X, screenInfo, err := Newconn(displayString)
	if err != nil {
		return nil, 0, err
	}

	chromeLayouts, inputWindowLayout := WindowsLayout(screenInfo, n)

	for i := 1; i <= n; i++ {
		cmd := ChromeCommand(chromeLayouts[i-1], fmt.Sprintf("%s/.aso_sxs_viewer/profiles/dir%d", os.Getenv("HOME"), i), displayString, debuggingport+i)
		go session.CreateChromeWindow(cmd, ctxCh, cmdErrorHandler)
	}

	return session.CreateInputWindow(inputWindowLayout, X, screenInfo)
}

func Newconn(displayString string) (*xgb.Conn, *xproto.ScreenInfo, error) {
	X, err := xgb.NewConnDisplay(displayString)
	if err != nil {
		return nil, nil, err
	}

	setup := xproto.Setup(X)
	screenInfo := setup.DefaultScreen(X)

	return X, screenInfo, nil
}

func establishChromeConnection(programState *command.ProgramState, timeout int) (context.Context, error) {
	wsURL, err := command.WsURL(programState, timeout)
	if err != nil {
		return nil, fmt.Errorf("Could not connect to the chrome window. Encountered error %s", err.Error())
	}

	if wsURL == "" {
		return nil, errors.New("must specify -devtools-ws-url")
	}

	allocatorContext, _ := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	// create context
	ctx, _ := chromedp.NewContext(allocatorContext)

	return ctx, nil
}

func DefaultXephyrSize() (height, width uint16) {
	height, width = 900, 1600 // Sensible defaults in case the below fails.

	X, err := xgb.NewConn()
	if err != nil {
		log.Println(err)
		return
	}
	if err := randr.Init(X); err != nil {
		log.Println(err)
		return
	}

	screens, err := randr.GetScreenResourcesCurrent(X, xproto.Setup(X).DefaultScreen(X).Root).Reply()
	if err != nil {
		log.Println(err)
		return
	}

	crtc, err := randr.GetCrtcInfo(X, screens.Crtcs[0], xproto.TimeCurrentTime).Reply()
	if err != nil {
		log.Println(err)
		return
	}
	return uint16(0.8 * float64(crtc.Height)), uint16(0.8 * float64(crtc.Width))
}

// WindowsLayout stores window size and position
func WindowsLayout(screenInfo *xproto.ScreenInfo, n int) (chromeLayouts []Layout, inputwindow Layout) {
	heightScreen, widthScreen := 0.8*screenInfo.HeightInPixels, screenInfo.WidthInPixels
	inputwindow.h, inputwindow.w = uint16(0.2*screenInfo.HeightInPixels), uint16(widthScreen)
	inputwindow.y = uint32(heightScreen)

	rows := int(n/4) + 1
	columns := math.Ceil(n / rows)

	var temp Layout
	temp.h = uint16(heightScreen / rows)
	temp.w = uint16(widthScreen / columns)

	for i, r := 0, rows; r > 0; r-- {
		temp.y = r * temp.h
		for c := columns; c > 0 && i < n; c-- {
			temp.x = c * temp.w
			chromeLayouts = append(chromeLayouts, temp)
			i++
		}
	}
	return chromeLayouts, inputwindow
}
