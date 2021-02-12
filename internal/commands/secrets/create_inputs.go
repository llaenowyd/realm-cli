package secrets

import (
	"github.com/10gen/realm-cli/internal/cli"
	"github.com/10gen/realm-cli/internal/terminal"

	"github.com/AlecAivazis/survey/v2"
)

const (
	flagName      = "name"
	flagNameShort = "n"
	flagNameUsage = `the name of the secret to add to your Realm App`

	flagValue      = "value"
	flagValueShort = "v"
	flagValueUsage = `the value of the secret to add to your Realm App`

	createInputFieldSecretName  = "name"
	createInputFieldSecretValue = "value"
)

type createInputs struct {
	cli.ProjectInputs
	Name  string
	Value string
}

func (i *createInputs) Resolve(profile *cli.Profile, ui terminal.UI) error {
	var questions []*survey.Question

	if i.Name == "" {
		questions = append(questions, &survey.Question{
			Name:   createInputFieldSecretName,
			Prompt: &survey.Input{Message: "Secret Name"},
		})
	}

	if i.Value == "" {
		questions = append(questions, &survey.Question{
			Name:   createInputFieldSecretValue,
			Prompt: &survey.Password{Message: "Secret Value"},
		})
	}

	if len(questions) > 0 {
		return ui.Ask(i, questions...)
	}
	return nil
}