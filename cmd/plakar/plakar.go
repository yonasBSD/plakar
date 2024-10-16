package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"path"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/PlakarLabs/plakar/encryption"
	"github.com/PlakarLabs/plakar/logger"
	"github.com/PlakarLabs/plakar/profiler"
	"github.com/PlakarLabs/plakar/repository"
	"github.com/PlakarLabs/plakar/storage"
	"github.com/denisbrodbeck/machineid"
	"github.com/dustin/go-humanize"
	"github.com/google/uuid"

	_ "github.com/PlakarLabs/plakar/storage/backends/database"
	_ "github.com/PlakarLabs/plakar/storage/backends/fs"
	_ "github.com/PlakarLabs/plakar/storage/backends/http"
	_ "github.com/PlakarLabs/plakar/storage/backends/null"
	_ "github.com/PlakarLabs/plakar/storage/backends/plakard"
	_ "github.com/PlakarLabs/plakar/storage/backends/s3"

	_ "github.com/PlakarLabs/plakar/snapshot/importer/fs"
	_ "github.com/PlakarLabs/plakar/snapshot/importer/ftp"
	_ "github.com/PlakarLabs/plakar/snapshot/importer/s3"

	_ "github.com/PlakarLabs/plakar/snapshot/exporter/fs"
	_ "github.com/PlakarLabs/plakar/snapshot/exporter/s3"
)

type Plakar struct {
	NumCPU      int
	Hostname    string
	Username    string
	Repository  string
	CommandLine string
	MachineID   string

	HomeDir string

	KeyFromFile string

	//	maxConcurrency chan struct{}
}

var commands map[string]func(Plakar, *repository.Repository, []string) int = make(map[string]func(Plakar, *repository.Repository, []string) int)

func registerCommand(command string, fn func(Plakar, *repository.Repository, []string) int) {
	commands[command] = fn
}

func executeCommand(ctx Plakar, repo *repository.Repository, command string, args []string) (int, error) {
	fn, exists := commands[command]
	if !exists {
		return 1, fmt.Errorf("unknown command: %s", command)
	}
	return fn(ctx, repo, args), nil
}

func main() {
	os.Exit(entryPoint())
}

func entryPoint() int {
	// default values
	opt_cpuDefault := runtime.GOMAXPROCS(0)
	if opt_cpuDefault != 1 {
		opt_cpuDefault = opt_cpuDefault - 1
	}

	opt_userDefault, err := user.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: go away casper !\n", flag.CommandLine.Name())
		return 1
	}

	opt_hostnameDefault, err := os.Hostname()
	if err != nil {
		opt_hostnameDefault = "localhost"
	}

	opt_machineIdDefault, err := machineid.ID()
	if err != nil {
		opt_machineIdDefault = uuid.NewSHA1(uuid.Nil, []byte(opt_hostnameDefault)).String()
	}
	opt_machineIdDefault = strings.ToLower(opt_machineIdDefault)

	opt_usernameDefault := opt_userDefault.Username
	opt_repositoryDefault := path.Join(opt_userDefault.HomeDir, ".plakar")
	opt_configDefault := path.Join(opt_userDefault.HomeDir, ".plakarconfig")

	// command line overrides
	var opt_cpuCount int
	var opt_configfile string
	var opt_username string
	var opt_hostname string
	var opt_cpuProfile string
	var opt_memProfile string
	var opt_time bool
	var opt_trace string
	var opt_verbose bool
	var opt_profiling bool
	var opt_keyfile string
	var opt_stats int

	flag.StringVar(&opt_configfile, "config", opt_configDefault, "configuration file")
	flag.IntVar(&opt_cpuCount, "cpu", opt_cpuDefault, "limit the number of usable cores")
	flag.StringVar(&opt_username, "username", opt_usernameDefault, "default username")
	flag.StringVar(&opt_hostname, "hostname", opt_hostnameDefault, "default hostname")
	flag.StringVar(&opt_cpuProfile, "profile-cpu", "", "profile CPU usage")
	flag.StringVar(&opt_memProfile, "profile-mem", "", "profile MEM usage")
	flag.BoolVar(&opt_time, "time", false, "display command execution time")
	flag.StringVar(&opt_trace, "trace", "", "display trace logs")
	flag.BoolVar(&opt_verbose, "verbose", false, "display verbose logs")
	flag.BoolVar(&opt_profiling, "profiling", false, "display profiling logs")
	flag.StringVar(&opt_keyfile, "keyfile", "", "use passphrase from key file when prompted")
	flag.IntVar(&opt_stats, "stats", 0, "display statistics")
	flag.Parse()

	// best effort check if security or reliability fix have been issued
	if rus, err := checkUpdate(); err == nil {
		if rus.SecurityFix || rus.ReliabilityFix {
			concerns := ""
			if rus.SecurityFix {
				concerns = "security"
			}
			if rus.ReliabilityFix {
				if concerns != "" {
					concerns += " and "
				}
				concerns += "reliability"
			}
			fmt.Fprintf(os.Stderr, "WARNING: %s concerns affect your current version, please upgrade to %s (+%d releases).\n", concerns, rus.Latest, rus.FoundCount)
		}
	}

	// setup from default + override
	if opt_cpuCount > runtime.NumCPU() {
		fmt.Fprintf(os.Stderr, "%s: can't use more cores than available: %d\n", flag.CommandLine.Name(), runtime.NumCPU())
		return 1
	}

	runtime.GOMAXPROCS(opt_cpuCount)

	if opt_cpuProfile != "" {
		f, err := os.Create(opt_cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not create CPU profile: %s\n", flag.CommandLine.Name(), err)
			return 1
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not start CPU profile: %s\n", flag.CommandLine.Name(), err)
			return 1
		}
		defer pprof.StopCPUProfile()
	}

	var secretFromKeyfile string
	if opt_keyfile != "" {
		data, err := os.ReadFile(opt_keyfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not read key file: %s\n", flag.CommandLine.Name(), err)
			return 1
		}
		secretFromKeyfile = strings.TrimSuffix(string(data), "\n")
	}

	ctx := Plakar{}
	ctx.NumCPU = opt_cpuCount
	ctx.Username = opt_username
	ctx.Hostname = opt_hostname
	ctx.Repository = opt_repositoryDefault
	ctx.CommandLine = strings.Join(os.Args, " ")
	ctx.MachineID = opt_machineIdDefault
	ctx.KeyFromFile = secretFromKeyfile
	ctx.HomeDir = opt_userDefault.HomeDir

	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "%s: a command must be provided\n", flag.CommandLine.Name())
		return 1
	}

	// start logging
	if opt_verbose {
		logger.EnableInfo()
	}
	if opt_trace != "" {
		logger.EnableTrace(opt_trace)
	}
	if opt_profiling {
		logger.EnableProfiling()
	}
	loggerWait := logger.Start()

	command, args := flag.Args()[0], flag.Args()[1:]

	//	if command == "agent" {
	//		return cmd_agent(ctx, args)
	//	}

	if flag.Arg(0) == "on" {
		if len(flag.Args()) < 2 {
			log.Fatalf("%s: missing plakar repository", flag.CommandLine.Name())
		}
		if len(flag.Args()) < 3 {
			log.Fatalf("%s: missing command", flag.CommandLine.Name())
		}
		ctx.Repository = flag.Arg(1)
		command, args = flag.Arg(2), flag.Args()[3:]
	} else {
		repositoryPath := os.Getenv("PLAKAR_REPOSITORY")
		if repositoryPath != "" {
			ctx.Repository = repositoryPath
		}
	}

	// cmd_create must be ran after workdir.New() but before other commands
	if command == "create" {
		return cmd_create(ctx, args)
	}

	//	if command == "config" {
	//		return cmd_config(ctx, args)
	//	}

	if command == "version" {
		return cmd_version(ctx, args)
	}

	if command == "stdio" {
		return cmd_stdio(ctx, args)
	}

	// special case, server does not need a cache but does not return immediately either
	skipPassphrase := false
	if command == "server" {
		skipPassphrase = true
	}

	store, err := storage.Open(ctx.Repository)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", flag.CommandLine.Name(), err)
		return 1
	}

	if store.Configuration().Version != storage.VERSION {
		fmt.Fprintf(os.Stderr, "%s: incompatible repository version: %s != %s\n",
			flag.CommandLine.Name(), store.Configuration().Version, storage.VERSION)
		return 1
	}

	var secret []byte
	if !skipPassphrase {
		if store.Configuration().Encryption != "" {
			envPassphrase := os.Getenv("PLAKAR_PASSPHRASE")
			if ctx.KeyFromFile == "" {
				attempts := 0
				for {
					var passphrase []byte
					if envPassphrase == "" {
						passphrase, err = getPassphrase("repository")
						if err != nil {
							fmt.Fprintf(os.Stderr, "%s\n", err)
							continue
						}
					} else {
						passphrase = []byte(envPassphrase)
					}

					secret, err = encryption.DeriveSecret(passphrase, store.Configuration().EncryptionKey)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s\n", err)
						attempts++

						if envPassphrase != "" {
							os.Exit(1)
						}
						continue
					}
					break
				}
			} else {
				secret, err = encryption.DeriveSecret([]byte(ctx.KeyFromFile), store.Configuration().EncryptionKey)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", err)
					os.Exit(1)
				}
			}
		}
	}

	//
	store.SetUsername(ctx.Username)
	store.SetHostname(ctx.Hostname)
	store.SetCommandLine(ctx.CommandLine)
	store.SetMachineID(ctx.MachineID)

	done := make(chan bool, 1)
	if opt_stats > 0 {
		go func() {
			iterCount := 0

			avgGoroutines := 0
			maxGoroutines := 0
			totalGoroutines := 0

			maxCgoCalls := int64(0)
			maxMemAlloc := uint64(0)
			avgMemAlloc := uint64(0)

			t0 := time.Now()

			lastrbytes := uint64(0)
			lastwbytes := uint64(0)

			for {
				if iterCount != 0 {

					elapsedSeconds := time.Since(t0).Seconds()

					rbytes := store.GetRBytes()
					wbytes := store.GetWBytes()

					rbytesAvg := rbytes / uint64(elapsedSeconds)
					wbytesAvg := wbytes / uint64(elapsedSeconds)

					diffrbytes := rbytes - lastrbytes
					diffwbytes := wbytes - lastwbytes
					lastrbytes = rbytes
					lastwbytes = wbytes

					var memStats runtime.MemStats
					runtime.ReadMemStats(&memStats)

					if runtime.NumGoroutine() > maxGoroutines {
						maxGoroutines = runtime.NumGoroutine()
					}
					totalGoroutines += runtime.NumGoroutine()
					avgGoroutines = totalGoroutines / int(elapsedSeconds)

					if runtime.NumCgoCall() > maxCgoCalls {
						maxCgoCalls = runtime.NumCgoCall()
					}
					if memStats.Alloc > maxMemAlloc {
						maxMemAlloc = memStats.Alloc
					}
					avgMemAlloc = memStats.TotalAlloc / uint64(iterCount)

					logger.Printf("[stats] cpu: goroutines: %d (μ %d, <= %d), cgocalls: %d (<= %d) | mem: %s (μ %s, <= %s, += %s), gc: %d | storage: rd: %s (μ %s, += %s), wr: %s (μ %s, += %s)",
						runtime.NumGoroutine(),
						avgGoroutines,
						maxGoroutines,
						runtime.NumCgoCall(),
						maxCgoCalls,
						humanize.Bytes(memStats.Alloc),
						humanize.Bytes(avgMemAlloc),
						humanize.Bytes(maxMemAlloc),
						humanize.Bytes(memStats.TotalAlloc),
						memStats.NumGC,
						humanize.Bytes(diffrbytes), humanize.Bytes(rbytesAvg), humanize.Bytes(rbytes),
						humanize.Bytes(diffwbytes), humanize.Bytes(wbytesAvg), humanize.Bytes(wbytes))
				}

				select {
				case <-time.After(time.Duration(opt_stats) * time.Second):
					iterCount++
					continue
				case <-done:
					return
				}
			}
		}()
	}

	repo, err := repository.New(store, secret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", flag.CommandLine.Name(), err)
		return 1
	}

	// commands below all operate on an open repository
	t0 := time.Now()
	status, err := executeCommand(ctx, repo, command, args)
	t1 := time.Since(t0)
	done <- true

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", flag.CommandLine.Name(), err)
	}

	err = store.Close()
	if err != nil {
		logger.Warn("could not close repository: %s", err)
	}

	if opt_profiling {
		profiler.Display()
	}

	loggerWait()

	if opt_time {
		fmt.Println("time:", t1)
	}

	if opt_memProfile != "" {
		f, err := os.Create(opt_memProfile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not write MEM profile: %d\n", flag.CommandLine.Name(), err)
			return 1
		}
	}

	return status
}
