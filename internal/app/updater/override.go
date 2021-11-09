package updater

import (
	"fmt"
	"os"
	"path"

	"github.com/alv91/helm-repo-updater/internal/app/yq"
	"github.com/argoproj-labs/argocd-image-updater/ext/git"
	"github.com/argoproj-labs/argocd-image-updater/pkg/log"
)

var _ changeWriter = writeOverrides

type changeWriter func(cfg HelmUpdaterConfig, gitC git.Client) (err error, skip bool, apps []Change)

//writeOverrides writes the overrides to the git files
func writeOverrides(cfg HelmUpdaterConfig, gitC git.Client) (err error, skip bool, apps []Change) {
	targetFile := path.Join(gitC.Root(), cfg.GitConf.File, cfg.File)

	_, err = os.Stat(targetFile)
	if err != nil {
		log.WithContext().
			AddField("application",cfg.AppName).
			Errorf("target file %s doesn't exist.", cfg.File)

		return err, true, nil
	}

	apps, err = overrideValues(cfg, targetFile)
	if err == fmt.Errorf("no changes") {
		return fmt.Errorf("target and marshaled keys for all targets are the same, skipping commit."), true, nil
	}

	if len(apps) == 0 {
		return fmt.Errorf("nothing to update, skipping commit."), true, nil
	}

	err = gitC.Add(targetFile)

	return nil, false, apps
}

// overrideValues overrides values in the given file
func overrideValues(cfg HelmUpdaterConfig, targetFile string) ([]Change, error) {
	var noChange int
	var err error

	apps := make([]Change, 0)
	logCtx := log.WithContext().AddField("application", cfg.AppName)

	for _, app := range cfg.UpdateApps {
		// define new entry
		var newEntry Change
		var presentedValue, patchedValue string

		// replace helm parameters
		presentedValue = yq.ReadKey(app.Key, targetFile)
		if presentedValue == "" {
			logCtx.Infof("failed to read the presented key %s, skipping change", app.Key)

			continue
		}

		newEntry.Key = app.Key
		newEntry.OldValue = presentedValue

		// replace helm parameters
		logCtx.Infof("Setting new value for key %s: %s", app.Key, app.NewValue)
		err = yq.InplaceApply(app.Key, app.NewValue, targetFile)
		if err != nil {
			logCtx.Infof("failed to update key %s: %v", app.Key, err)

			newEntry.NewValue = presentedValue

			continue
		}

		// check patched app
		patchedValue = yq.ReadKey(app.Key, targetFile)
		if patchedValue == "" {
			logCtx.Infof("failed to read the patched key %s, skipping change", app.Key)

			newEntry.NewValue = presentedValue

			continue
		}
		newEntry.NewValue = patchedValue

		// check if there is any change
		if presentedValue != patchedValue {
			apps = append(apps, newEntry)
		} else {
			logCtx.Infof("target for key %s is the same, skipping", app.Key)

			noChange++
		}
	}

	if noChange == len(cfg.UpdateApps) {
		return nil, fmt.Errorf("no changes")
	}

	return apps, nil
}