//go:build windows

package smtc

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"uika-resonance/internal/wrt"
)

const (
	classManager    = "Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager"
	classDataReader = "Windows.Storage.Streams.DataReader"

	iidStatics      = "2050c4ee-11a0-57de-aed7-c97c70338245"
	iidManager      = "cace8eac-e86e-504a-ab31-5ff8ff1bce49"
	iidSession      = "7148c835-9b14-5ae2-ab85-dc9b1c14e1a8"
	iidMediaProps   = "68856cf6-adb4-54b2-ac16-05837907acb6"
	iidPlaybackInfo = "94b4b6cf-e8ba-51ad-87a7-c10ade106127"
	iidTimeline     = "ede34136-6f25-588d-8ecf-ea5b6735aaa5"
	iidRASRef       = "33ee3134-1dd6-4e3a-8067-d1c162e8642b"
	iidRAS          = "905a0fe1-bc53-11df-8c49-001e4fc686da"
	iidDataRdrFac   = "d7527847-57da-4e15-914c-06806699a098"
	iidRASWCT       = "cc254827-4b3d-438f-9232-10c76bc7e038"
)

const (
	StatusClosed = 0
	StatusOpened = 1
	StatusChang  = 2
	StatusStop   = 3
	StatusPlay   = 4
	StatusPaused = 5
)

var (
	iidAsyncOpManager = sync.OnceValue(func() *wrt.GUID {
		return wrt.AsyncOperationIID(wrt.SigClass("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager", iidManager))
	})
	iidAsyncOpMediaProps = sync.OnceValue(func() *wrt.GUID {
		return wrt.AsyncOperationIID(wrt.SigClass("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties", iidMediaProps))
	})
	iidAsyncOpRASWCT = sync.OnceValue(func() *wrt.GUID {
		return wrt.AsyncOperationIID(wrt.SigInterface(iidRASWCT))
	})
	iidAsyncOpU32 = sync.OnceValue(func() *wrt.GUID {
		return wrt.AsyncOperationIID(wrt.SigU32())
	})
	iidVectorSession = sync.OnceValue(func() *wrt.GUID {
		return wrt.VectorViewIID(wrt.SigClass("Windows.Media.Control.GlobalSystemMediaTransportControlsSession", iidSession))
	})
)

type Track struct {
	AppID       string
	Title       string
	Subtitle    string
	Artist      string
	AlbumTitle  string
	AlbumArtist string
	TrackNumber int32
	Playing     bool
	Status      int32

	PositionSec float64
	StartSec    float64
	DurationSec float64
	LastUpdated time.Time

	Thumb func() ([]byte, error)
}

func (t Track) StatusName() string {
	switch t.Status {
	case StatusPlay:
		return "playing"
	case StatusPaused:
		return "paused"
	case StatusStop:
		return "stopped"
	case StatusOpened:
		return "opened"
	case StatusChang:
		return "changing"
	default:
		return "closed"
	}
}

type Manager struct {
	mgr *wrt.Object
}

// factoryCache memoizes a WinRT activation-factory lookup. RoGetActivationFactory
// builds an HSTRING and crosses into the WinRT activation layer on every call,
// while a class's factory is process-stable, so resolving it more than once only
// costs time. Only success is cached: a transient activation failure is still
// retried by the next Connect, exactly as it was before.
//
// It is a named type so the memoization can be tested without invoking WinRT.
type factoryCache struct {
	mu   sync.Mutex
	obj  *wrt.Object
	done bool
}

func (c *factoryCache) get(load func() (*wrt.Object, error)) (*wrt.Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return c.obj, nil
	}
	obj, err := load()
	if err != nil {
		return nil, err
	}
	c.obj, c.done = obj, true
	return obj, nil
}

// managerStatics holds the SMTC manager activation factory for the life of the
// process, so it is deliberately never released.
var managerStatics factoryCache

func managerFactory() (*wrt.Object, error) {
	return managerStatics.get(func() (*wrt.Object, error) {
		return wrt.GetActivationFactory(classManager, wrt.MustGUID(iidStatics))
	})
}

func Connect() (*Manager, error) {
	runtime.LockOSThread()
	if err := wrt.ComInit(); err != nil {
		return nil, err
	}
	statics, err := managerFactory()
	if err != nil {
		return nil, err
	}
	op, err := wrt.CallAsync(statics, 6, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("smtc: RequestAsync: %w", err)
	}
	defer op.Release()
	var raw unsafe.Pointer
	if err := wrt.GetResults(op, iidAsyncOpManager(), unsafe.Pointer(&raw)); err != nil {
		return nil, fmt.Errorf("smtc: GetResults(manager): %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("smtc: manager is null")
	}
	return &Manager{mgr: wrt.NewObject(raw)}, nil
}

func (m *Manager) Close() {
	if m != nil && m.mgr != nil {
		m.mgr.Release()
		m.mgr = nil
	}
}

type Watcher struct {
	Interval time.Duration
	Out      chan []Track

	onErr func(error)
}

func NewWatcher(interval time.Duration) *Watcher {
	return &Watcher{Interval: interval, Out: make(chan []Track, 1)}
}

func (w *Watcher) OnError(f func(error)) { w.onErr = f }

func publishLatest(out chan []Track, tracks []Track) {
	select {
	case out <- tracks:
		return
	default:
	}

	select {
	case <-out:
	default:
	}
	select {
	case out <- tracks:
	default:
	}
}

func (w *Watcher) Run(ctx context.Context) {
	runtime.LockOSThread()
	for {
		if ctx.Err() != nil {
			return
		}
		m, err := Connect()
		if err != nil {
			if w.onErr != nil {
				w.onErr(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		tick := time.NewTicker(w.Interval)
		run := func() {
			defer tick.Stop()
			for {
				if ctx.Err() != nil {
					return
				}

				tracks, err := m.Sessions()
				if err != nil {
					if w.onErr != nil {
						w.onErr(err)
					}
					return
				}
				publishLatest(w.Out, tracks)
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}
		run()
		m.Close()
		if ctx.Err() != nil {
			return
		}
	}
}

func ReadThumbnail(ref *wrt.Object) ([]byte, error) {
	_ = wrt.ComInit()
	op, err := wrt.CallAsync(ref, 6, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("smtc thumb: open: %w", err)
	}
	defer op.Release()
	var stRaw unsafe.Pointer
	if err := wrt.GetResults(op, iidAsyncOpRASWCT(), unsafe.Pointer(&stRaw)); err != nil {
		return nil, fmt.Errorf("smtc thumb: results: %w", err)
	}
	if stRaw == nil {
		return nil, fmt.Errorf("smtc thumb: null stream")
	}
	stream := wrt.NewObject(stRaw)
	defer stream.Release()

	ras, err := stream.QueryInterface(wrt.MustGUID(iidRAS))
	if err != nil {
		return nil, fmt.Errorf("smtc thumb: QI IRandomAccessStream: %w", err)
	}
	defer ras.Release()

	size, _ := wrt.CallU64Getter(ras, 6)
	if size == 0 || size > 16<<20 {
		return nil, fmt.Errorf("smtc thumb: bad size %d", size)
	}
	var inRaw unsafe.Pointer
	if _, err := wrt.VtCall(ras, 8, 0, uintptr(unsafe.Pointer(&inRaw))); err != nil || inRaw == nil {
		return nil, fmt.Errorf("smtc thumb: input stream: %w", err)
	}
	inStream := wrt.NewObject(inRaw)
	defer inStream.Release()

	factory, err := dataReaderFactory()
	if err != nil {
		return nil, err
	}
	var rdrRaw unsafe.Pointer
	if _, err := wrt.VtCall(factory, 6, inStream.RawPtr(), uintptr(unsafe.Pointer(&rdrRaw))); err != nil || rdrRaw == nil {
		return nil, fmt.Errorf("smtc thumb: CreateDataReader: %w", err)
	}
	reader := wrt.NewObject(rdrRaw)
	defer reader.Release()

	op2, err := wrt.CallAsyncObj(reader, 29, uintptr(size), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("smtc thumb: load: %w", err)
	}
	defer op2.Release()
	var loaded uint32
	if err := wrt.GetResults(op2, iidAsyncOpU32(), unsafe.Pointer(&loaded)); err != nil {
		return nil, fmt.Errorf("smtc thumb: load results: %w", err)
	}
	if loaded == 0 || uint64(loaded) > size {
		return nil, fmt.Errorf("smtc thumb: bad loaded %d", loaded)
	}
	buf := make([]byte, loaded)
	if _, err := wrt.VtCall(reader, 14, uintptr(loaded), uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		return nil, fmt.Errorf("smtc thumb: read: %w", err)
	}
	return buf, nil
}

var (
	rdrFacOnce sync.Once
	rdrFac     *wrt.Object
	rdrFacErr  error
)

func dataReaderFactory() (*wrt.Object, error) {
	rdrFacOnce.Do(func() {
		rdrFac, rdrFacErr = wrt.GetActivationFactory(classDataReader, wrt.MustGUID(iidDataRdrFac))
	})
	return rdrFac, rdrFacErr
}

func Classify(appID string) string {
	a := strings.ToLower(appID)
	switch {
	case strings.Contains(a, "apple"):
		return "applemusic"
	case strings.Contains(a, "spotify"):
		return "spotify"
	case strings.Contains(a, "chrome"), strings.Contains(a, "msedge"),
		strings.Contains(a, "edge"), strings.Contains(a, "firefox"),
		strings.Contains(a, "brave"), strings.Contains(a, "opera"),
		strings.Contains(a, "vivaldi"), strings.Contains(a, "arc"):
		return "browser"
	default:
		return "generic"
	}
}

func FriendlyName(appID string) string {
	switch Classify(appID) {
	case "applemusic":
		return "Apple Music"
	case "spotify":
		return "Spotify"
	case "browser":
		return "Browser"
	default:
		if i := strings.LastIndex(appID, "."); i >= 0 && i+1 < len(appID) {
			return appID[i+1:]
		}
		return appID
	}
}

func (m *Manager) eachSession(fn func(i uint32, sess *wrt.Object) bool) error {
	if m == nil || m.mgr == nil {
		return fmt.Errorf("smtc: not connected")
	}
	vec, err := wrt.CallObjectGetter(m.mgr, 7)
	if err != nil {
		return err
	}
	defer vec.Release()

	views, err := vec.QueryInterface(iidVectorSession())
	if err != nil {
		return fmt.Errorf("smtc: QI IVectorView<Session>: %w", err)
	}
	defer views.Release()

	n, err := wrt.CallU32Getter(views, 7)
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		var raw unsafe.Pointer
		if _, err := wrt.VtCall(views, 6, uintptr(i), uintptr(unsafe.Pointer(&raw))); err != nil || raw == nil {
			continue
		}
		sess := wrt.NewObject(raw)
		next := fn(i, sess)
		sess.Release()
		if !next {
			break
		}
	}
	return nil
}

func (m *Manager) Sessions() ([]Track, error) {
	var tracks []Track
	err := m.eachSession(func(i uint32, sess *wrt.Object) bool {
		if tr, err := m.session(sess); err == nil {
			tracks = append(tracks, tr)
		} else {
			log.Printf("smtc: session %d: %v", i, err)
		}
		return true
	})
	return tracks, err
}

func (m *Manager) thumbnail(appID string) ([]byte, error) {
	var data []byte
	found := false
	err := m.eachSession(func(_ uint32, sess *wrt.Object) bool {
		id, err := wrt.CallStringGetter(sess, 6)
		if err != nil || id != appID {
			return true
		}
		found = true
		data, err = readSessionThumbnail(sess)
		return false
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("smtc thumb: session %q gone", appID)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("smtc thumb: empty")
	}
	return data, nil
}

func readSessionThumbnail(sess *wrt.Object) ([]byte, error) {
	op, err := wrt.CallAsync(sess, 7, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("media properties: %w", err)
	}
	defer op.Release()
	var mpRaw unsafe.Pointer
	if err := wrt.GetResults(op, iidAsyncOpMediaProps(), unsafe.Pointer(&mpRaw)); err != nil {
		return nil, fmt.Errorf("media properties results: %w", err)
	}
	mp := wrt.NewObject(mpRaw)
	defer mp.Release()
	ref, err := wrt.CallObjectGetter(mp, 15)
	if err != nil {
		return nil, err
	}
	defer ref.Release()
	return ReadThumbnail(ref)
}

func (m *Manager) session(sess *wrt.Object) (Track, error) {
	appID, err := wrt.CallStringGetter(sess, 6)
	if err != nil {
		return Track{}, err
	}
	t := Track{AppID: appID}

	if pb, err := wrt.CallObjectGetter(sess, 9); err == nil {
		t.Status, _ = wrt.CallI32Getter(pb, 7)
		pb.Release()
		t.Playing = t.Status == StatusPlay
	}

	if tp, err := wrt.CallObjectGetter(sess, 8); err == nil {
		defer tp.Release()
		start, _ := wrt.CallI64Getter(tp, 6)
		end, _ := wrt.CallI64Getter(tp, 7)
		pos, _ := wrt.CallI64Getter(tp, 10)
		lu, _ := wrt.CallI64Getter(tp, 11)
		t.StartSec = ticksToSecs(start)
		t.DurationSec = ticksToSecs(end) - t.StartSec
		if t.DurationSec < 0 {
			t.DurationSec = 0
		}
		t.PositionSec = ticksToSecs(pos)
		t.LastUpdated = filetimeToTime(lu)
	}

	op, err := wrt.CallAsync(sess, 7, 3*time.Second)
	if err != nil {
		return t, fmt.Errorf("media properties: %w", err)
	}
	defer op.Release()
	var mpRaw unsafe.Pointer
	if err := wrt.GetResults(op, iidAsyncOpMediaProps(), unsafe.Pointer(&mpRaw)); err != nil {
		return t, fmt.Errorf("media properties results: %w", err)
	}
	mp := wrt.NewObject(mpRaw)
	defer mp.Release()

	t.Title, _ = wrt.CallStringGetter(mp, 6)
	t.Subtitle, _ = wrt.CallStringGetter(mp, 7)
	t.AlbumArtist, _ = wrt.CallStringGetter(mp, 8)
	t.Artist, _ = wrt.CallStringGetter(mp, 9)
	t.AlbumTitle, _ = wrt.CallStringGetter(mp, 10)
	t.TrackNumber, _ = wrt.CallI32Getter(mp, 11)

	t.Thumb = sync.OnceValues(func() ([]byte, error) { return m.thumbnail(appID) })
	return t, nil
}

func Diagnose() {
	_ = wrt.ComInit()
	statics, err := wrt.GetActivationFactory(classManager, wrt.MustGUID(iidStatics))
	if err != nil {
		fmt.Println("diagnose: factory:", err)
		return
	}
	defer statics.Release()
	op, err := wrt.CallAsync(statics, 6, 3*time.Second)
	if err != nil {
		fmt.Println("diagnose: request:", err)
		return
	}
	defer op.Release()
	sig := wrt.SigClass("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager", iidManager)
	for i, v := range wrt.ParameterizedIIDVariants(wrt.BaseIAsyncOperation, sig) {
		if o, err := op.QueryInterface(wrt.MustGUID(v)); err == nil {
			fmt.Printf("diagnose: variant %d MATCH %s\n", i, v)
			o.Release()
		} else {
			fmt.Printf("diagnose: variant %d no (%s)\n", i, v)
		}
	}
}

func ticksToSecs(ticks int64) float64 { return float64(ticks) / 1e7 }

func filetimeToTime(v int64) time.Time {

	const epochDelta = 116444736000000000
	if v <= epochDelta {
		return time.Time{}
	}
	return time.Unix((v-epochDelta)/1e7, ((v-epochDelta)%1e7)*100).UTC()
}
