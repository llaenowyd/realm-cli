package push

import (
	"os"
	"strings"
	"time"

	"github.com/10gen/realm-cli/internal/cli"
	"github.com/10gen/realm-cli/internal/cloud/realm"
	"github.com/10gen/realm-cli/internal/local"
	"github.com/10gen/realm-cli/internal/terminal"

	"github.com/AlecAivazis/survey/v2"
	"github.com/briandowns/spinner"
	"github.com/spf13/pflag"
)

// Command is the `push` command
type Command struct {
	inputs inputs
}

// Flags is the command flags
func (cmd *Command) Flags(fs *pflag.FlagSet) {
	fs.StringVarP(&cmd.inputs.AppDirectory, flagAppDirectory, flagAppDirectoryShort, "", flagAppDirectoryUsage)
	fs.StringVar(&cmd.inputs.Project, flagProject, "", flagProjectUsage)
	fs.StringVarP(&cmd.inputs.To, flagTo, flagToShort, "", flagToUsage)
	fs.BoolVarP(&cmd.inputs.AsNew, flagAsNew, flagAsNewShort, false, flagAsNewUsage)
	fs.BoolVarP(&cmd.inputs.DryRun, flagDryRun, flagDryRunShort, false, flagDryRunUsage)
	fs.BoolVarP(&cmd.inputs.IncludeDependencies, flagIncludeDependencies, flagIncludeDependenciesShort, false, flagIncludeDependenciesUsage)
	fs.BoolVarP(&cmd.inputs.IncludeHosting, flagIncludeHosting, flagIncludeHostingShort, false, flagIncludeHostingUsage)
	fs.BoolVarP(&cmd.inputs.ResetCDNCache, flagResetCDNCache, flagResetCDNCacheShort, false, flagResetCDNCacheUsage)
}

// Inputs is the command inputs
func (cmd *Command) Inputs() cli.InputResolver {
	return &cmd.inputs
}

// Handler is the command handler
func (cmd *Command) Handler(profile *cli.Profile, ui terminal.UI, clients cli.Clients) error {
	app, err := local.LoadApp(cmd.inputs.AppDirectory)
	if err != nil {
		return err
	}

	to, err := cmd.inputs.resolveTo(ui, clients.Realm)
	if err != nil {
		return err
	}

	if to.GroupID == "" {
		groupID, err := cli.ResolveGroupID(ui, clients.Atlas)
		if err != nil {
			return err
		}
		to.GroupID = groupID
	}

	var isNewApp bool
	if to.AppID == "" {
		if cmd.inputs.DryRun {
			ui.Print(
				terminal.NewTextLog("This is a new app. To create a new app, you must omit the 'dry-run' flag to proceed"),
				terminal.NewFollowupLog(terminal.MsgSuggestedCommands, cmd.commandString(true)),
			)
			return nil
		}

		app, proceed, err := createNewApp(ui, clients.Realm, cmd.inputs.AppDirectory, to.GroupID, app.AppData)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}

		to.AppID = app.ID
		isNewApp = true
	}

	ui.Print(terminal.NewTextLog("Determining changes"))
	appDiffs, err := clients.Realm.Diff(to.GroupID, to.AppID, app.AppData)
	if err != nil {
		return err
	}

	hosting, err := local.FindAppHosting(app.RootDir)
	if err != nil {
		return err
	}

	var hostingDiffs local.HostingDiffs
	if cmd.inputs.IncludeHosting {
		appAssets, err := clients.Realm.HostingAssets(to.GroupID, to.AppID)
		if err != nil {
			return err
		}

		hostingDiffs, err = hosting.Diffs(profile.HostingAssetCachePath(), to.AppID, appAssets)
		if err != nil {
			return err
		}
	}

	if len(appDiffs) == 0 && !cmd.inputs.IncludeDependencies && hostingDiffs.Size() == 0 {
		ui.Print(terminal.NewTextLog("Deployed app is identical to proposed version, nothing to do"))
		return nil
	}

	if !ui.AutoConfirm() && !isNewApp {
		diffs := make([]string, 0, len(appDiffs)+1+hostingDiffs.Cap())

		diffs = append(diffs, appDiffs...)

		if cmd.inputs.IncludeDependencies {
			// TODO(REALMC-8242): diff dependencies better
			diffs = append(diffs, "+ New function dependencies")
		}

		diffs = append(diffs, hostingDiffs.Strings()...)

		// when updating an existing app, if the user has not set the '-y' flag
		// print the app diffs back to the user
		ui.Print(terminal.NewTextLog(
			"The following reflects the proposed changes to your Realm app\n%s",
			strings.Join(diffs, "\n"),
		))
	}

	if cmd.inputs.DryRun {
		ui.Print(
			terminal.NewTextLog("To push these changes, you must omit the 'dry-run' flag to proceed"),
			terminal.NewFollowupLog(terminal.MsgSuggestedCommands, cmd.commandString(true)),
		)
		return nil
	}

	proceed, err := ui.Confirm("Please confirm the changes shown above")
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	ui.Print(terminal.NewTextLog("Creating draft"))
	draft, proceed, err := createNewDraft(ui, clients.Realm, to)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	ui.Print(terminal.NewTextLog("Pushing changes"))
	if err := clients.Realm.Import(to.GroupID, to.AppID, app.AppData); err != nil {
		return err
	}

	ui.Print(terminal.NewTextLog("Deploying draft"))
	if err := deployDraftAndWait(ui, clients.Realm, to, draft.ID); err != nil {
		return err
	}

	if cmd.inputs.IncludeDependencies {
		dependencies, err := local.FindAppDependencies(app.RootDir)
		if err != nil {
			return err
		}

		s := spinner.New(terminal.SpinnerCircles, 250*time.Millisecond)
		s.Suffix = " Transpiling dependency sources..."

		prepareUpload := func() (string, error) {
			s.Start()
			defer s.Stop()

			path, err := dependencies.PrepareUpload()
			if err != nil {
				return "", err
			}

			ui.Print(terminal.NewTextLog("Transpiled dependency sources"))
			return path, nil
		}

		uploadPath, err := prepareUpload()
		if err != nil {
			return err
		}
		defer os.Remove(uploadPath) //nolint:errcheck

		if err := clients.Realm.ImportDependencies(to.GroupID, to.AppID, uploadPath); err != nil {
			return err
		}
		ui.Print(terminal.NewTextLog("Uploaded dependencies archive"))
	}

	if cmd.inputs.IncludeHosting {
		s := spinner.New(terminal.SpinnerCircles, 250*time.Millisecond)
		s.Suffix = " Importing hosting assets..."

		importHosting := func() error {
			s.Start()
			defer s.Stop()

			return hosting.UploadHostingAssets(
				clients.Realm,
				to.GroupID,
				to.AppID,
				hostingDiffs,
				func(err error) { ui.Print(terminal.NewWarningLog(err.Error())) },
			)
		}

		if err := importHosting(); err != nil {
			return err
		}
		ui.Print(terminal.NewTextLog("Import hosting assets"))

		if cmd.inputs.ResetCDNCache {
			s := spinner.New(terminal.SpinnerCircles, 250*time.Millisecond)
			s.Suffix = " Resetting CDN cache..."

			invalidateCache := func() error {
				s.Start()
				defer s.Stop()

				return clients.Realm.HostingCacheInvalidate(to.GroupID, to.AppID, "/*")
			}

			if err := invalidateCache(); err != nil {
				return err
			}
			ui.Print(terminal.NewTextLog("Reset CDN cache"))
		}
	}

	ui.Print(terminal.NewTextLog("Successfully pushed app up: %s", app.ID()))
	return nil
}

type namer interface{ Name() string }
type locationer interface{ Location() realm.Location }
type deploymentModeler interface{ DeploymentModel() realm.DeploymentModel }

func createNewApp(ui terminal.UI, realmClient realm.Client, appDirectory, groupID string, appData interface{}) (realm.App, bool, error) {
	if proceed, err := ui.Confirm("Do you wish to create a new app?"); err != nil {
		return realm.App{}, false, err
	} else if !proceed {
		return realm.App{}, false, nil
	}

	var name, location, deploymentModel string
	if appData != nil {
		if n, ok := appData.(namer); ok {
			name = n.Name()
		}

		if l, ok := appData.(locationer); ok {
			location = l.Location().String()
		}

		if dm, ok := appData.(deploymentModeler); ok {
			deploymentModel = dm.DeploymentModel().String()
		}
	}

	if name == "" || !ui.AutoConfirm() {
		if err := ui.AskOne(&name, &survey.Input{Message: "App Name", Default: name}); err != nil {
			return realm.App{}, false, err
		}
	}

	if !ui.AutoConfirm() {
		if err := ui.AskOne(
			&location,
			&survey.Select{
				Message: "App Location",
				Options: realm.LocationValues,
				Default: location,
			},
		); err != nil {
			return realm.App{}, false, err
		}
	}

	if !ui.AutoConfirm() {
		if err := ui.AskOne(
			&deploymentModel,
			&survey.Select{
				Message: "App Deployment Model",
				Options: realm.DeploymentModelValues,
				Default: deploymentModel,
			}); err != nil {
			return realm.App{}, false, err
		}
	}

	app, err := realmClient.CreateApp(
		groupID,
		name,
		realm.AppMeta{Location: realm.Location(location), DeploymentModel: realm.DeploymentModel(deploymentModel)},
	)
	if err != nil {
		return realm.App{}, false, err
	}

	if err := local.AsApp(appDirectory, app).WriteConfig(); err != nil {
		return realm.App{}, false, err
	}
	return app, true, nil
}

func createNewDraft(ui terminal.UI, realmClient realm.Client, to to) (realm.AppDraft, bool, error) {
	draft, draftErr := realmClient.CreateDraft(to.GroupID, to.AppID)
	if draftErr == nil {
		return draft, true, nil
	}

	if err, ok := draftErr.(realm.ServerError); !ok || err.Code != realm.ErrCodeDraftAlreadyExists {
		return realm.AppDraft{}, false, draftErr
	}

	existingDraft, existingDraftErr := realmClient.Draft(to.GroupID, to.AppID)
	if existingDraftErr != nil {
		return realm.AppDraft{}, false, existingDraftErr
	}

	if !ui.AutoConfirm() {
		if err := diffDraft(ui, realmClient, to, existingDraft.ID); err != nil {
			return realm.AppDraft{}, false, err
		}

		proceed, proceedErr := ui.Confirm("Would you like to discard this draft?")
		if proceedErr != nil {
			return realm.AppDraft{}, false, proceedErr
		}
		if !proceed {
			return realm.AppDraft{}, false, nil
		}
	}

	if err := realmClient.DiscardDraft(to.GroupID, to.AppID, existingDraft.ID); err != nil {
		return realm.AppDraft{}, false, err
	}

	draft, draftErr = realmClient.CreateDraft(to.GroupID, to.AppID)
	return draft, true, draftErr
}

func diffDraft(ui terminal.UI, realmClient realm.Client, to to, draftID string) error {
	diff, diffErr := realmClient.DiffDraft(to.GroupID, to.AppID, draftID)
	if diffErr != nil {
		return diffErr
	}

	var logs []terminal.Log
	if !diff.HasChanges() {
		logs = append(logs, terminal.NewTextLog("An empty draft already exists for your app"))
	} else {
		logs = append(logs, terminal.NewListLog("The following draft already exists for your app...", diff.DiffList()...))
		if diff.HostingFilesDiff.HasChanges() {
			logs = append(logs, terminal.NewListLog("With changes to your static hosting files...", diff.HostingFilesDiff.DiffList()...))
		}
		if diff.DependenciesDiff.HasChanges() {
			logs = append(logs, terminal.NewListLog("With changes to your app dependencies...", diff.DependenciesDiff.DiffList()...))
		}
		if diff.GraphQLConfigDiff.HasChanges() {
			logs = append(logs, terminal.NewListLog("With changes to your GraphQL configuration...", diff.GraphQLConfigDiff.DiffList()...))
		}
		if diff.SchemaOptionsDiff.HasChanges() {
			logs = append(logs, terminal.NewListLog("With changes to your app schema...", diff.SchemaOptionsDiff.DiffList()...))
		}
	}
	ui.Print(logs...)
	return nil
}

func deployDraftAndWait(ui terminal.UI, realmClient realm.Client, to to, draftID string) error {
	deployment, err := realmClient.DeployDraft(to.GroupID, to.AppID, draftID)
	if err != nil {
		return err
	}

	s := spinner.New(terminal.SpinnerCircles, 250*time.Millisecond)
	s.Suffix = " Deploying app changes..."

	waitForDeployment := func() error {
		s.Start()
		defer s.Stop()

		for deployment.Status == realm.DeploymentStatusCreated || deployment.Status == realm.DeploymentStatusPending {
			time.Sleep(time.Second)

			deployment, err = realmClient.Deployment(to.GroupID, to.AppID, deployment.ID)
			if err != nil {
				if e := realmClient.DiscardDraft(to.GroupID, to.AppID, draftID); e != nil {
					ui.Print(terminal.NewWarningLog("Failed to discard the draft created for your deployment"))
				}
				return err
			}
		}

		return nil
	}

	if err := waitForDeployment(); err != nil {
		return err
	}

	ui.Print(terminal.NewTextLog("Deployment complete"))
	return nil
}

func (cmd *Command) commandString(omitDryRun bool) string {
	sb := strings.Builder{}
	sb.WriteString("realm-cli push")

	// TODO(REALMC-7866): make this more accurate based on the inputs provided

	if cmd.inputs.DryRun && !omitDryRun {
		sb.WriteString(" --dry-run")
	}

	return sb.String()
}