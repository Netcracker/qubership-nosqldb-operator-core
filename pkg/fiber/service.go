package fiber

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/gofiber/fiber/v2"
)

type FiberServiceError struct {
	Msg string
}

func (r *FiberServiceError) Error() string {
	return r.Msg
}

type FiberServerExists struct {
	error
	Msg string
}

func (r *FiberServerExists) Error() string {
	return r.Msg
}

var once sync.Once
var fiberService FiberService

func GetFiberService() *FiberService {
	once.Do(func() {
		log.Print("Intantiate fiberService in singleton")
		fiberService = &FiberServiceImpl{servers: sync.Map{}}
	})
	return &fiberService
}

type FiberService interface {
	Create(port int, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error
	CreateTLS(port int, crtPath string, keyPath string, isTLSEnabled bool, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error
	Shutdown(port int) error
	CheckServerExists(port int) bool
	createFiberServer(port int, crtPath string, keyPath string, isTLSEnabled bool, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error
}

type FiberServiceInternal interface {
	FiberService
	ShutdownAll() []error
}

type fiberServer struct {
	Server  *fiber.App
	Context context.Context
	Cancel  context.CancelFunc
}

type FiberServiceImpl struct {
	servers sync.Map
}

// TODO debug logs to find where stuck
func (f *FiberServiceImpl) Create(port int, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error {
	return f.createFiberServer(port, "", "", false, setUp, forceShutdown...)
}

func (f *FiberServiceImpl) Shutdown(port int) error {
	if server, ok := f.servers.Load(port); ok {
		server.(*fiberServer).Server.Shutdown()
		server.(*fiberServer).Cancel()
		f.servers.Delete(port)
		return nil
	}
	return fmt.Errorf("server not found on port %d", port)
}

func (f *FiberServiceImpl) CheckServerExists(port int) bool {
	if _, ok := f.servers.Load(port); ok {
		return true
	}
	return false
}

func (f *FiberServiceImpl) ShutdownAll() []error {
	var errs []error
	if isSyncMapEmpty(&f.servers) {
		return nil
	}
	f.servers.Range(func(key, value any) bool {
		err := value.(*fiberServer).Server.Shutdown()
		value.(*fiberServer).Cancel()
		if err != nil {
			errs = append(errs, err)
		}
		return true
	})

	return errs
}

func (f *FiberServiceImpl) CreateTLS(port int, crtPath string, keyPath string, isTLSEnabled bool, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error {
	return f.createFiberServer(port, crtPath, keyPath, isTLSEnabled, setUp, forceShutdown...)
}

func (f *FiberServiceImpl) createFiberServer(port int, crtPath string, keyPath string, isTLSEnabled bool, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error {
	log.Print("Starting to create new fiber server")
	if f.CheckServerExists(port) {
		log.Printf("Stopping server on port %d", port)
		if len(forceShutdown) > 0 && forceShutdown[0] {
			err := f.Shutdown(port)
			if err != nil {
				return err
			}
		} else {
			return &FiberServerExists{Msg: "Server on " + strconv.Itoa(port) + " port already presented"}
		}
	}

	log.Print("No running servers left, create a new one")
	serverCtx, cancel := context.WithCancel(context.Background())
	app := fiber.New()

	setupErr := setUp(app, serverCtx)
	if setupErr != nil {
		cancel()
		return setupErr
	}

	log.Print("Calling new go routin")
	go func() {
		var err error
		if isTLSEnabled {
			err = app.ListenTLS(":"+strconv.Itoa(port), crtPath, keyPath)
		} else {
			err = app.Listen(":" + strconv.Itoa(port))
		}
		if err != nil {
			log.Printf("Error during go routin start %v", err)
			cancel()
			f.Shutdown(port)
			log.Printf("Server on %v port is failed and removed from servers list: %+v", port, err)
		}
	}()

	f.servers.Store(port, &fiberServer{
		Server:  app,
		Context: serverCtx,
		Cancel:  cancel,
	})

	return nil
}

func isSyncMapEmpty(m *sync.Map) bool {
	isEmpty := true
	m.Range(func(_, _ interface{}) bool {
		isEmpty = false
		return false // stop iterating
	})
	return isEmpty
}
