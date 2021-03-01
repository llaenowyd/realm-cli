package app

import (
	"archive/zip"
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/10gen/realm-cli/internal/cli"
	"github.com/10gen/realm-cli/internal/cloud/realm"
	"github.com/10gen/realm-cli/internal/local"
	"github.com/10gen/realm-cli/internal/utils/test/assert"
	"github.com/10gen/realm-cli/internal/utils/test/mock"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestAppInitHandler(t *testing.T) {
	t.Run("should initialize an empty project when no from type is specified", func(t *testing.T) {
		profile, teardown := mock.NewProfileFromTmpDir(t, "app_init_test")
		defer teardown()

		out, ui := mock.NewUI()

		cmd := &CommandInit{initInputs{newAppInputs{
			Name:            "test-app",
			Project:         "test-project",
			DeploymentModel: realm.DeploymentModelLocal,
			Location:        realm.LocationSydney,
		}}}

		assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{}))

		data, readErr := ioutil.ReadFile(filepath.Join(profile.WorkingDirectory, local.FileRealmConfig.String()))
		assert.Nil(t, readErr)

		assert.Equal(t, "01:23:45 UTC INFO  Successfully initialized app\n", out.String())

		var config local.AppRealmConfigJSON
		assert.Nil(t, json.Unmarshal(data, &config))
		assert.Equal(t, local.AppRealmConfigJSON{local.AppDataV2{local.AppStructureV2{
			ConfigVersion:   realm.DefaultAppConfigVersion,
			Name:            "test-app",
			Location:        realm.LocationSydney,
			DeploymentModel: realm.DeploymentModelLocal,
		}}}, config)
	})

	t.Run("should initialze a templated app when from type is specified to app", func(t *testing.T) {
		profile, teardown := mock.NewProfileFromTmpDir(t, "app_init_test")
		defer teardown()

		out, ui := mock.NewUI()

		app := realm.App{
			ID:          primitive.NewObjectID().Hex(),
			GroupID:     primitive.NewObjectID().Hex(),
			ClientAppID: "test-app-abcde",
			Name:        "test-app",
		}

		var zipCloser *zip.ReadCloser
		defer func() { zipCloser.Close() }()

		client := mock.RealmClient{}
		client.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return []realm.App{app}, nil
		}
		client.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			zipPkg, err := zip.OpenReader("testdata/project.zip")

			zipCloser = zipPkg
			return "", &zipPkg.Reader, err
		}

		cmd := &CommandInit{initInputs{newAppInputs{From: "test"}}}

		assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{Realm: client}))

		assert.Equal(t, "01:23:45 UTC INFO  Successfully initialized app\n", out.String())

		t.Run("should have the expected contents in the app config file", func(t *testing.T) {
			data, readErr := ioutil.ReadFile(filepath.Join(profile.WorkingDirectory, local.FileRealmConfig.String()))
			assert.Nil(t, readErr)

			var config local.AppRealmConfigJSON
			assert.Nil(t, json.Unmarshal(data, &config))
			assert.Equal(t, local.AppRealmConfigJSON{local.AppDataV2{local.AppStructureV2{
				ConfigVersion:   realm.DefaultAppConfigVersion,
				Name:            "from-app",
				Location:        realm.LocationIreland,
				DeploymentModel: realm.DeploymentModelGlobal,
			}}}, config)
		})

		// TODO(REALMC-7886): once a full, minimal app is initialized, uncomment this test
		// 		t.Run("should have the expected contents in the auth custom user data file", func(t *testing.T) {
		// 			config, err := ioutil.ReadFile(filepath.Join(profile.WorkingDirectory, local.NameAuth, local.FileCustomUserData.String()))
		// 			assert.Nil(t, err)
		// 			assert.Equal(t, `{
		//     "enabled": false
		// }
		// `, string(config))
		// 		})

		// TODO(REALMC-7886): once a full, minimal app is initialized, uncomment this test
		// 		t.Run("should have the expected contents in the auth providers file", func(t *testing.T) {
		// 			config, err := ioutil.ReadFile(filepath.Join(profile.WorkingDirectory, local.NameAuth, local.FileProviders.String()))
		// 			assert.Nil(t, err)
		// 			assert.Equal(t, `{
		//     "api-key": {
		//         "name": "api-key",
		//         "type": "api-key",
		//         "enabled": false
		//     },
		// }
		// `, string(config))
		// 		})

		// TODO(REALMC-7886): once a full, minimal app is initialized, uncomment this test
		// 		t.Run("should have the expected contents in the sync config file", func(t *testing.T) {
		// 			config, err := ioutil.ReadFile(filepath.Join(profile.WorkingDirectory, local.NameAuth, local.FileCustomUserData.String()))
		// 			assert.Nil(t, err)
		// 			assert.Equal(t, `{
		//     "development_mode_enabled": false
		// }
		// `, string(config))
		// 		})

		// TODO(REALMC-7886): once a full, minimal app is initialized, implement these tests
		// should have an empty http_endpoints directory
		// should have an empty data_sources directory
		// should have an empty services directory
	})
}