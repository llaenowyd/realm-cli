package app

import (
	"strings"

	"github.com/10gen/realm-cli/internal/cli"
	"github.com/10gen/realm-cli/internal/local"
	"github.com/10gen/realm-cli/internal/terminal"

	"github.com/spf13/pflag"
)

const (
	flagIncludeDependencies      = "include-dependencies"
	flagIncludeDependenciesShort = "d"
	flagIncludeDependenciesUsage = "include to diff Realm app dependencies changes as well"
	flagIncludeHosting           = "include-hosting"
	flagIncludeHostingShort      = "s"
	flagIncludeHostingUsage      = "include to diff Realm app hosting changes as well"
)

type diffInputs struct {
	AppDirectory        string
	IncludeDependencies bool
	IncludeHosting      bool
	cli.ProjectInputs
}

func (i *diffInputs) Resolve(profile *cli.Profile, ui terminal.UI) error {
	if i.AppDirectory == "" {
		i.AppDirectory = profile.WorkingDirectory
	}
	return nil
}

// CommandDiff is the `app diff` command
type CommandDiff struct {
	inputs diffInputs
}

// Flags is the command flags
func (cmd *CommandDiff) Flags(fs *pflag.FlagSet) {
	fs.StringVarP(&cmd.inputs.AppDirectory, flagDirectory, flagDirectoryShort, "", flagDirectoryUsage)
	fs.BoolVarP(&cmd.inputs.IncludeDependencies, flagIncludeDependencies, flagIncludeDependenciesShort, false, flagIncludeDependenciesUsage)
	fs.BoolVarP(&cmd.inputs.IncludeHosting, flagIncludeHosting, flagIncludeHostingShort, false, flagIncludeHostingUsage)

	cmd.inputs.Flags(fs)
}

// Inputs is the command inputs
func (cmd *CommandDiff) Inputs() cli.InputResolver {
	return &cmd.inputs
}

// Handler is the command handler
func (cmd *CommandDiff) Handler(profile *cli.Profile, ui terminal.UI, clients cli.Clients) error {
	app, err := local.LoadApp(cmd.inputs.AppDirectory)
	if err != nil {
		return err
	}

	appToDiff, err := cli.ResolveApp(ui, clients.Realm, cmd.inputs.Filter())
	if err != nil {
		return err
	}

	diffs, err := clients.Realm.Diff(appToDiff.GroupID, appToDiff.ID, app.AppData)
	if err != nil {
		return err
	}

	if cmd.inputs.IncludeDependencies {
		// TODO(REALMC-8242): diff dependencies better
		diffs = append(diffs, "+ New function dependencies")
	}

	if cmd.inputs.IncludeHosting {
		hosting, err := local.FindAppHosting(app.RootDir)
		if err != nil {
			return err
		}

		appAssets, err := clients.Realm.HostingAssets(appToDiff.GroupID, appToDiff.ID)
		if err != nil {
			return err
		}

		hostingDiffs, err := hosting.Diffs(profile.HostingAssetCachePath(), appToDiff.ID, appAssets)
		if err != nil {
			return err
		}

		diffs = append(diffs, hostingDiffs.Strings()...)
	}

	if len(diffs) == 0 {
		// there are no diffs
		ui.Print(terminal.NewTextLog("Deployed app is identical to proposed version"))
		return nil
	}

	ui.Print(terminal.NewTextLog(
		"The following reflects the proposed changes to your Realm app\n%s",
		strings.Join(diffs, "\n"),
	))

	return nil
}