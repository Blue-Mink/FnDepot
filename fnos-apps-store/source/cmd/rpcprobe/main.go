//go:build linux

// Command rpcprobe exercises the fnOS app-center daemon's private RPC channel
// through this repo's own platform layer.
//
// Why it exists: the install/update/uninstall channel the store depends on is
// undocumented and fnOS can change it under us. A bash re-implementation of the
// protocol (fnos-apps/test/rpc.sh) drifts from the Go client silently — it has
// no equivalent of the transient-poll tolerance in rpc.go, for instance. This
// probe calls the SHIPPING code, so a post-upgrade run proves the store itself
// still works rather than proving a copy of it does.
//
// Build and run on the box (never on macOS — the daemon only exists on fnOS):
//
//	GOOS=linux GOARCH=amd64 go build -o /tmp/rpcprobe ./cmd/rpcprobe
//	scp /tmp/rpcprobe fnos-test:/tmp/ && ssh fnos-test 'sudo /tmp/rpcprobe capability'
//
// The install/upgrade/uninstall subcommands MUTATE the box. Point them at a
// disposable app on a disposable VM.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fnos-store/internal/platform"
)

const usage = `rpcprobe <command> [args]

Read-only:
  capability                       UpgradeCapability + DaemonUpgradeAvailable
  stage <fpk>                      StageFpk (daemon unpacks + identifies)
  wizard <fpk>                     FetchWizard (install/info or update/info)
  list                             List installed apps
  check <app>                      Is the app installed?
  status <app>                     Runtime status
  volumes                          ListVolumes
  appvolume <app>                  AppInstallVolume

Mutating (disposable VM only):
  install <fpk> <volume> [k=v,..]  InstallFpkWithWizard
  upgrade <fpk> [k=v,..]           UpgradeFpk (data-preserving)
  uninstall <app>                  Uninstall (keeps @appdata)`

// probeTimeout bounds a whole run. Install and upgrade poll daemon tasks that
// legitimately take minutes on a slow volume.
const probeTimeout = 20 * time.Minute

func parseParams(s string) []platform.WizardParam {
	out := []platform.WizardParam{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out = append(out, platform.WizardParam{Key: strings.TrimSpace(k), Value: v})
	}
	return out
}

func dump(label string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("%s = <unencodable: %v>\n", label, err)
		return
	}
	fmt.Printf("%s = %s\n", label, b)
}

// arg returns positional argument i (0-based after the subcommand), or an
// error naming what was missing, so a typo reports the flag instead of panicking.
func arg(i int, name string) (string, error) {
	if len(os.Args) <= i+2 {
		return "", fmt.Errorf("missing required argument <%s>", name)
	}
	return os.Args[i+2], nil
}

func optArg(i int) string {
	if len(os.Args) <= i+2 {
		return ""
	}
	return os.Args[i+2]
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(2)
	}

	ac := platform.NewLinuxAppCenter()
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	start := time.Now()
	err := run(ctx, ac)
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		fmt.Printf("RESULT=FAIL (%s)\nerr: %v\n", elapsed, err)
		// Surface the daemon's own code: 10030 validation, 10100 package not
		// staged, 19000 a required wizard field was not supplied.
		var de *platform.DaemonError
		if errors.As(err, &de) {
			fmt.Printf("daemonError: path=%s code=%d msg=%q\n", de.Path, de.Code, de.Msg)
		}
		os.Exit(1)
	}
	fmt.Printf("RESULT=OK (%s)\n", elapsed)
}

func run(ctx context.Context, ac *platform.LinuxAppCenter) error {
	switch os.Args[1] {
	case "capability":
		fmt.Printf("DaemonUpgradeAvailable = %v\n", ac.DaemonUpgradeAvailable())
		dump("UpgradeCapability", ac.UpgradeCapability())
		return nil

	case "stage":
		fpk, err := arg(0, "fpk")
		if err != nil {
			return err
		}
		staged, err := ac.StageFpk(ctx, fpk)
		if err != nil {
			return err
		}
		dump("StagedPackage", staged)
		return nil

	case "wizard":
		fpk, err := arg(0, "fpk")
		if err != nil {
			return err
		}
		w, err := ac.FetchWizard(ctx, fpk)
		if err != nil {
			return err
		}
		dump("AppWizard", w)
		return nil

	case "install":
		fpk, err := arg(0, "fpk")
		if err != nil {
			return err
		}
		volArg, err := arg(1, "volume")
		if err != nil {
			return err
		}
		vol, err := strconv.Atoi(volArg)
		if err != nil {
			return fmt.Errorf("volume must be an integer, got %q", volArg)
		}
		return ac.InstallFpkWithWizard(ctx, fpk, vol, parseParams(optArg(2)))

	case "upgrade":
		fpk, err := arg(0, "fpk")
		if err != nil {
			return err
		}
		return ac.UpgradeFpk(ctx, fpk, parseParams(optArg(1)))

	case "uninstall":
		app, err := arg(0, "app")
		if err != nil {
			return err
		}
		return ac.Uninstall(ctx, app)

	case "list":
		apps, err := ac.List()
		if err != nil {
			return err
		}
		fmt.Printf("installed = %d\n", len(apps))
		for _, a := range apps {
			fmt.Printf("  %-24s %-20s %s\n", a.AppName, a.Version, a.Status)
		}
		return nil

	case "check":
		app, err := arg(0, "app")
		if err != nil {
			return err
		}
		ok, err := ac.Check(app)
		if err != nil {
			return err
		}
		fmt.Printf("Check(%s) = %v\n", app, ok)
		return nil

	case "status":
		app, err := arg(0, "app")
		if err != nil {
			return err
		}
		st, err := ac.Status(app)
		if err != nil {
			return err
		}
		fmt.Printf("Status(%s) = %q\n", app, st)
		return nil

	case "volumes":
		vols, err := ac.ListVolumes()
		if err != nil {
			return err
		}
		dump("Volumes", vols)
		return nil

	case "appvolume":
		app, err := arg(0, "app")
		if err != nil {
			return err
		}
		idx, found, err := ac.AppInstallVolume(app)
		if err != nil {
			return err
		}
		fmt.Printf("AppInstallVolume(%s) = %d found=%v\n", app, idx, found)
		return nil

	default:
		fmt.Println(usage)
		os.Exit(2)
		return nil
	}
}
