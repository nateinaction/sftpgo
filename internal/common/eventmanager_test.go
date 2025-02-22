// Copyright (C) 2019-2022  Nicola Murino
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package common

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sftpgo/sdk"
	sdkkms "github.com/sftpgo/sdk/kms"
	"github.com/stretchr/testify/assert"

	"github.com/drakkan/sftpgo/v2/internal/dataprovider"
	"github.com/drakkan/sftpgo/v2/internal/kms"
	"github.com/drakkan/sftpgo/v2/internal/util"
	"github.com/drakkan/sftpgo/v2/internal/vfs"
)

func TestEventRuleMatch(t *testing.T) {
	conditions := dataprovider.EventConditions{
		ProviderEvents: []string{"add", "update"},
		Options: dataprovider.ConditionOptions{
			Names: []dataprovider.ConditionPattern{
				{
					Pattern:      "user1",
					InverseMatch: true,
				},
			},
		},
	}
	res := eventManager.checkProviderEventMatch(conditions, EventParams{
		Name:  "user1",
		Event: "add",
	})
	assert.False(t, res)
	res = eventManager.checkProviderEventMatch(conditions, EventParams{
		Name:  "user2",
		Event: "update",
	})
	assert.True(t, res)
	res = eventManager.checkProviderEventMatch(conditions, EventParams{
		Name:  "user2",
		Event: "delete",
	})
	assert.False(t, res)
	conditions.Options.ProviderObjects = []string{"api_key"}
	res = eventManager.checkProviderEventMatch(conditions, EventParams{
		Name:       "user2",
		Event:      "update",
		ObjectType: "share",
	})
	assert.False(t, res)
	res = eventManager.checkProviderEventMatch(conditions, EventParams{
		Name:       "user2",
		Event:      "update",
		ObjectType: "api_key",
	})
	assert.True(t, res)
	// now test fs events
	conditions = dataprovider.EventConditions{
		FsEvents: []string{operationUpload, operationDownload},
		Options: dataprovider.ConditionOptions{
			Names: []dataprovider.ConditionPattern{
				{
					Pattern: "user*",
				},
				{
					Pattern: "tester*",
				},
			},
			FsPaths: []dataprovider.ConditionPattern{
				{
					Pattern: "*.txt",
				},
			},
			Protocols:   []string{ProtocolSFTP},
			MinFileSize: 10,
			MaxFileSize: 30,
		},
	}
	params := EventParams{
		Name:        "tester4",
		Event:       operationDelete,
		VirtualPath: "/path.txt",
		Protocol:    ProtocolSFTP,
		ObjectName:  "path.txt",
		FileSize:    20,
	}
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.Event = operationDownload
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.True(t, res)
	params.Name = "name"
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.Name = "user5"
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.True(t, res)
	params.VirtualPath = "/sub/f.jpg"
	params.ObjectName = path.Base(params.VirtualPath)
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.VirtualPath = "/sub/f.txt"
	params.ObjectName = path.Base(params.VirtualPath)
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.True(t, res)
	params.Protocol = ProtocolHTTP
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.Protocol = ProtocolSFTP
	params.FileSize = 5
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.FileSize = 50
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
	params.FileSize = 25
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.True(t, res)
	// bad pattern
	conditions.Options.Names = []dataprovider.ConditionPattern{
		{
			Pattern: "[-]",
		},
	}
	res = eventManager.checkFsEventMatch(conditions, params)
	assert.False(t, res)
}

func TestEventManager(t *testing.T) {
	startEventScheduler()
	action := &dataprovider.BaseEventAction{
		Name: "test_action",
		Type: dataprovider.ActionTypeHTTP,
		Options: dataprovider.BaseEventActionOptions{
			HTTPConfig: dataprovider.EventActionHTTPConfig{
				Endpoint: "http://localhost",
				Timeout:  20,
				Method:   http.MethodGet,
			},
		},
	}
	err := dataprovider.AddEventAction(action, "", "")
	assert.NoError(t, err)
	rule := &dataprovider.EventRule{
		Name:    "rule",
		Trigger: dataprovider.EventTriggerFsEvent,
		Conditions: dataprovider.EventConditions{
			FsEvents: []string{operationUpload},
		},
		Actions: []dataprovider.EventAction{
			{
				BaseEventAction: dataprovider.BaseEventAction{
					Name: action.Name,
				},
				Order: 1,
			},
		},
	}

	err = dataprovider.AddEventRule(rule, "", "")
	assert.NoError(t, err)

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 1)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	assert.Len(t, eventManager.schedulesMapping, 0)
	eventManager.RUnlock()

	rule.Trigger = dataprovider.EventTriggerProviderEvent
	rule.Conditions = dataprovider.EventConditions{
		ProviderEvents: []string{"add"},
	}
	err = dataprovider.UpdateEventRule(rule, "", "")
	assert.NoError(t, err)

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 1)
	assert.Len(t, eventManager.Schedules, 0)
	assert.Len(t, eventManager.schedulesMapping, 0)
	eventManager.RUnlock()

	rule.Trigger = dataprovider.EventTriggerSchedule
	rule.Conditions = dataprovider.EventConditions{
		Schedules: []dataprovider.Schedule{
			{
				Hours:      "0",
				DayOfWeek:  "*",
				DayOfMonth: "*",
				Month:      "*",
			},
		},
	}
	rule.DeletedAt = util.GetTimeAsMsSinceEpoch(time.Now().Add(-12 * time.Hour))
	eventManager.addUpdateRuleInternal(*rule)

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	assert.Len(t, eventManager.schedulesMapping, 0)
	eventManager.RUnlock()

	assert.Eventually(t, func() bool {
		_, err = dataprovider.EventRuleExists(rule.Name)
		_, ok := err.(*util.RecordNotFoundError)
		return ok
	}, 2*time.Second, 100*time.Millisecond)

	rule.DeletedAt = 0
	err = dataprovider.AddEventRule(rule, "", "")
	assert.NoError(t, err)

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 1)
	assert.Len(t, eventManager.schedulesMapping, 1)
	eventManager.RUnlock()

	err = dataprovider.DeleteEventRule(rule.Name, "", "")
	assert.NoError(t, err)

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	assert.Len(t, eventManager.schedulesMapping, 0)
	eventManager.RUnlock()

	err = dataprovider.DeleteEventAction(action.Name, "", "")
	assert.NoError(t, err)
	stopEventScheduler()
}

func TestEventManagerErrors(t *testing.T) {
	startEventScheduler()
	providerConf := dataprovider.GetProviderConfig()
	err := dataprovider.Close()
	assert.NoError(t, err)

	params := EventParams{
		sender: "sender",
	}
	_, err = params.getUsers()
	assert.Error(t, err)
	_, err = params.getFolders()
	assert.Error(t, err)

	err = executeUsersQuotaResetRuleAction(dataprovider.ConditionOptions{}, EventParams{})
	assert.Error(t, err)
	err = executeFoldersQuotaResetRuleAction(dataprovider.ConditionOptions{}, EventParams{})
	assert.Error(t, err)
	err = executeTransferQuotaResetRuleAction(dataprovider.ConditionOptions{}, EventParams{})
	assert.Error(t, err)
	err = executeQuotaResetForUser(dataprovider.User{
		Groups: []sdk.GroupMapping{
			{
				Name: "agroup",
				Type: sdk.GroupTypePrimary,
			},
		},
	})
	assert.Error(t, err)
	err = executeDataRetentionCheckForUser(dataprovider.User{
		Groups: []sdk.GroupMapping{
			{
				Name: "agroup",
				Type: sdk.GroupTypePrimary,
			},
		},
	}, nil)
	assert.Error(t, err)

	dataRetentionAction := dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeDataRetentionCheck,
		Options: dataprovider.BaseEventActionOptions{
			RetentionConfig: dataprovider.EventActionDataRetentionConfig{
				Folders: []dataprovider.FolderRetention{
					{
						Path:      "/",
						Retention: 24,
					},
				},
			},
		},
	}
	err = executeRuleAction(dataRetentionAction, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: "username1",
			},
		},
	})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "unable to get users")
	}

	eventManager.loadRules()

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	eventManager.RUnlock()

	// rule with invalid trigger
	eventManager.addUpdateRuleInternal(dataprovider.EventRule{
		Name:    "test rule",
		Trigger: -1,
	})

	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	eventManager.RUnlock()
	// rule with invalid cronspec
	eventManager.addUpdateRuleInternal(dataprovider.EventRule{
		Name:    "test rule",
		Trigger: dataprovider.EventTriggerSchedule,
		Conditions: dataprovider.EventConditions{
			Schedules: []dataprovider.Schedule{
				{
					Hours: "1000",
				},
			},
		},
	})
	eventManager.RLock()
	assert.Len(t, eventManager.FsEvents, 0)
	assert.Len(t, eventManager.ProviderEvents, 0)
	assert.Len(t, eventManager.Schedules, 0)
	eventManager.RUnlock()

	err = dataprovider.Initialize(providerConf, configDir, true)
	assert.NoError(t, err)
	stopEventScheduler()
}

func TestEventRuleActions(t *testing.T) {
	actionName := "test rule action"
	action := dataprovider.BaseEventAction{
		Name: actionName,
		Type: dataprovider.ActionTypeBackup,
	}
	err := executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{})
	assert.NoError(t, err)
	action.Type = -1
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{})
	assert.Error(t, err)

	action = dataprovider.BaseEventAction{
		Name: actionName,
		Type: dataprovider.ActionTypeHTTP,
		Options: dataprovider.BaseEventActionOptions{
			HTTPConfig: dataprovider.EventActionHTTPConfig{
				Endpoint:      "http://foo\x7f.com/", // invalid URL
				SkipTLSVerify: true,
				Body:          "{{ObjectData}}",
				Method:        http.MethodPost,
				QueryParameters: []dataprovider.KeyValue{
					{
						Key:   "param",
						Value: "value",
					},
				},
				Timeout: 5,
				Headers: []dataprovider.KeyValue{
					{
						Key:   "Content-Type",
						Value: "application/json",
					},
				},
				Username: "httpuser",
			},
		},
	}
	action.Options.SetEmptySecretsIfNil()
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "invalid endpoint")
	}
	action.Options.HTTPConfig.Endpoint = fmt.Sprintf("http://%v", httpAddr)
	params := EventParams{
		Name: "a",
		Object: &dataprovider.User{
			BaseUser: sdk.BaseUser{
				Username: "test user",
			},
		},
	}
	err = executeRuleAction(action, params, dataprovider.ConditionOptions{})
	assert.NoError(t, err)
	action.Options.HTTPConfig.Endpoint = fmt.Sprintf("http://%v/404", httpAddr)
	err = executeRuleAction(action, params, dataprovider.ConditionOptions{})
	if assert.Error(t, err) {
		assert.Equal(t, err.Error(), "unexpected status code: 404")
	}
	action.Options.HTTPConfig.Endpoint = "http://invalid:1234"
	err = executeRuleAction(action, params, dataprovider.ConditionOptions{})
	assert.Error(t, err)
	action.Options.HTTPConfig.QueryParameters = nil
	action.Options.HTTPConfig.Endpoint = "http://bar\x7f.com/"
	err = executeRuleAction(action, params, dataprovider.ConditionOptions{})
	assert.Error(t, err)
	action.Options.HTTPConfig.Password = kms.NewSecret(sdkkms.SecretStatusSecretBox, "payload", "key", "data")
	err = executeRuleAction(action, params, dataprovider.ConditionOptions{})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "unable to decrypt password")
	}
	// test disk and transfer quota reset
	username1 := "user1"
	username2 := "user2"
	user1 := dataprovider.User{
		BaseUser: sdk.BaseUser{
			Username: username1,
			HomeDir:  filepath.Join(os.TempDir(), username1),
			Status:   1,
			Permissions: map[string][]string{
				"/": {dataprovider.PermAny},
			},
		},
	}
	user2 := dataprovider.User{
		BaseUser: sdk.BaseUser{
			Username: username2,
			HomeDir:  filepath.Join(os.TempDir(), username2),
			Status:   1,
			Permissions: map[string][]string{
				"/": {dataprovider.PermAny},
			},
		},
	}
	err = dataprovider.AddUser(&user1, "", "")
	assert.NoError(t, err)
	err = dataprovider.AddUser(&user2, "", "")
	assert.NoError(t, err)

	action = dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeUserQuotaReset,
	}
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.Error(t, err) // no home dir
	// create the home dir
	err = os.MkdirAll(user1.GetHomeDir(), os.ModePerm)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(user1.GetHomeDir(), "file.txt"), []byte("user"), 0666)
	assert.NoError(t, err)
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.NoError(t, err)
	userGet, err := dataprovider.UserExists(username1)
	assert.NoError(t, err)
	assert.Equal(t, 1, userGet.UsedQuotaFiles)
	assert.Equal(t, int64(4), userGet.UsedQuotaSize)
	// simulate another quota scan in progress
	assert.True(t, QuotaScans.AddUserQuotaScan(username1))
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.Error(t, err)
	assert.True(t, QuotaScans.RemoveUserQuotaScan(username1))
	// non matching pattern
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: "don't match",
			},
		},
	})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no user quota reset executed")
	}

	dataRetentionAction := dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeDataRetentionCheck,
		Options: dataprovider.BaseEventActionOptions{
			RetentionConfig: dataprovider.EventActionDataRetentionConfig{
				Folders: []dataprovider.FolderRetention{
					{
						Path:      "",
						Retention: 24,
					},
				},
			},
		},
	}
	err = executeRuleAction(dataRetentionAction, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.Error(t, err) // invalid config, no folder path specified
	retentionDir := "testretention"
	dataRetentionAction = dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeDataRetentionCheck,
		Options: dataprovider.BaseEventActionOptions{
			RetentionConfig: dataprovider.EventActionDataRetentionConfig{
				Folders: []dataprovider.FolderRetention{
					{
						Path:            path.Join("/", retentionDir),
						Retention:       24,
						DeleteEmptyDirs: true,
					},
				},
			},
		},
	}
	// create some test files
	file1 := filepath.Join(user1.GetHomeDir(), "file1.txt")
	file2 := filepath.Join(user1.GetHomeDir(), retentionDir, "file2.txt")
	file3 := filepath.Join(user1.GetHomeDir(), retentionDir, "file3.txt")
	file4 := filepath.Join(user1.GetHomeDir(), retentionDir, "sub", "file4.txt")

	err = os.MkdirAll(filepath.Dir(file4), os.ModePerm)
	assert.NoError(t, err)

	for _, f := range []string{file1, file2, file3, file4} {
		err = os.WriteFile(f, []byte(""), 0666)
		assert.NoError(t, err)
	}
	timeBeforeRetention := time.Now().Add(-48 * time.Hour)
	err = os.Chtimes(file1, timeBeforeRetention, timeBeforeRetention)
	assert.NoError(t, err)
	err = os.Chtimes(file2, timeBeforeRetention, timeBeforeRetention)
	assert.NoError(t, err)
	err = os.Chtimes(file4, timeBeforeRetention, timeBeforeRetention)
	assert.NoError(t, err)

	err = executeRuleAction(dataRetentionAction, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.NoError(t, err)
	assert.FileExists(t, file1)
	assert.NoFileExists(t, file2)
	assert.FileExists(t, file3)
	assert.NoDirExists(t, filepath.Dir(file4))
	// simulate another check in progress
	c := RetentionChecks.Add(RetentionCheck{}, &user1)
	assert.NotNil(t, c)
	err = executeRuleAction(dataRetentionAction, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.Error(t, err)
	RetentionChecks.remove(user1.Username)

	err = executeRuleAction(dataRetentionAction, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: "no match",
			},
		},
	})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no retention check executed")
	}

	err = os.RemoveAll(user1.GetHomeDir())
	assert.NoError(t, err)

	err = dataprovider.UpdateUserTransferQuota(&user1, 100, 100, true)
	assert.NoError(t, err)

	action.Type = dataprovider.ActionTypeTransferQuotaReset
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: username1,
			},
		},
	})
	assert.NoError(t, err)
	userGet, err = dataprovider.UserExists(username1)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), userGet.UsedDownloadDataTransfer)
	assert.Equal(t, int64(0), userGet.UsedUploadDataTransfer)

	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: "no match",
			},
		},
	})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no transfer quota reset executed")
	}

	err = dataprovider.DeleteUser(username1, "", "")
	assert.NoError(t, err)
	err = dataprovider.DeleteUser(username2, "", "")
	assert.NoError(t, err)
	// test folder quota reset
	foldername1 := "f1"
	foldername2 := "f2"
	folder1 := vfs.BaseVirtualFolder{
		Name:       foldername1,
		MappedPath: filepath.Join(os.TempDir(), foldername1),
	}
	folder2 := vfs.BaseVirtualFolder{
		Name:       foldername2,
		MappedPath: filepath.Join(os.TempDir(), foldername2),
	}
	err = dataprovider.AddFolder(&folder1, "", "")
	assert.NoError(t, err)
	err = dataprovider.AddFolder(&folder2, "", "")
	assert.NoError(t, err)
	action = dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeFolderQuotaReset,
	}
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: foldername1,
			},
		},
	})
	assert.Error(t, err) // no home dir
	err = os.MkdirAll(folder1.MappedPath, os.ModePerm)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(folder1.MappedPath, "file.txt"), []byte("folder"), 0666)
	assert.NoError(t, err)
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: foldername1,
			},
		},
	})
	assert.NoError(t, err)
	folderGet, err := dataprovider.GetFolderByName(foldername1)
	assert.NoError(t, err)
	assert.Equal(t, 1, folderGet.UsedQuotaFiles)
	assert.Equal(t, int64(6), folderGet.UsedQuotaSize)
	// simulate another quota scan in progress
	assert.True(t, QuotaScans.AddVFolderQuotaScan(foldername1))
	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: foldername1,
			},
		},
	})
	assert.Error(t, err)
	assert.True(t, QuotaScans.RemoveVFolderQuotaScan(foldername1))

	err = executeRuleAction(action, EventParams{}, dataprovider.ConditionOptions{
		Names: []dataprovider.ConditionPattern{
			{
				Pattern: "no folder match",
			},
		},
	})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no folder quota reset executed")
	}

	err = os.RemoveAll(folder1.MappedPath)
	assert.NoError(t, err)
	err = dataprovider.DeleteFolder(foldername1, "", "")
	assert.NoError(t, err)
	err = dataprovider.DeleteFolder(foldername2, "", "")
	assert.NoError(t, err)
}

func TestFilesystemActionErrors(t *testing.T) {
	err := executeFsRuleAction(dataprovider.EventActionFilesystemConfig{}, EventParams{})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "unsupported filesystem action")
	}
	username := "test_user_for_actions"
	testReplacer := strings.NewReplacer("old", "new")
	err = executeDeleteFsAction(nil, testReplacer, username)
	assert.Error(t, err)
	err = executeMkDirsFsAction(nil, testReplacer, username)
	assert.Error(t, err)
	err = executeRenameFsAction(nil, testReplacer, username)
	assert.Error(t, err)

	user := dataprovider.User{
		BaseUser: sdk.BaseUser{
			Username: username,
			Permissions: map[string][]string{
				"/": {dataprovider.PermAny},
			},
			HomeDir: filepath.Join(os.TempDir(), username),
		},
		FsConfig: vfs.Filesystem{
			Provider: sdk.SFTPFilesystemProvider,
			SFTPConfig: vfs.SFTPFsConfig{
				BaseSFTPFsConfig: sdk.BaseSFTPFsConfig{
					Endpoint: "127.0.0.1:4022",
					Username: username,
				},
				Password: kms.NewPlainSecret("pwd"),
			},
		},
	}
	conn := NewBaseConnection("", protocolEventAction, "", "", user)
	err = executeDeleteFileFsAction(conn, "", nil)
	assert.Error(t, err)
	err = dataprovider.AddUser(&user, "", "")
	assert.NoError(t, err)
	// check root fs fails
	err = executeDeleteFsAction(nil, testReplacer, username)
	assert.Error(t, err)
	err = executeMkDirsFsAction(nil, testReplacer, username)
	assert.Error(t, err)
	err = executeRenameFsAction(nil, testReplacer, username)
	assert.Error(t, err)

	user.FsConfig.Provider = sdk.LocalFilesystemProvider
	user.Permissions["/"] = []string{dataprovider.PermUpload}
	err = dataprovider.DeleteUser(username, "", "")
	assert.NoError(t, err)
	err = dataprovider.AddUser(&user, "", "")
	assert.NoError(t, err)
	err = executeRenameFsAction([]dataprovider.KeyValue{
		{
			Key:   "/p1",
			Value: "/p1",
		},
	}, testReplacer, username)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "the rename source and target cannot be the same")
	}

	if runtime.GOOS != osWindows {
		dirPath := filepath.Join(user.HomeDir, "adir", "sub")
		err := os.MkdirAll(dirPath, os.ModePerm)
		assert.NoError(t, err)
		filePath := filepath.Join(dirPath, "f.dat")
		err = os.WriteFile(filePath, nil, 0666)
		assert.NoError(t, err)
		err = os.Chmod(dirPath, 0001)
		assert.NoError(t, err)

		err = executeDeleteFsAction([]string{"/adir/sub"}, testReplacer, username)
		assert.Error(t, err)
		err = executeDeleteFsAction([]string{"/adir/sub/f.dat"}, testReplacer, username)
		assert.Error(t, err)
		err = os.Chmod(dirPath, 0555)
		assert.NoError(t, err)
		err = executeDeleteFsAction([]string{"/adir/sub/f.dat"}, testReplacer, username)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "unable to remove file")
		}

		err = executeMkDirsFsAction([]string{"/adir/sub/sub"}, testReplacer, username)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "unable to create dir")
		}
		err = executeMkDirsFsAction([]string{"/adir/sub/sub/sub"}, testReplacer, username)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "unable to check parent dirs")
		}

		err = os.Chmod(dirPath, os.ModePerm)
		assert.NoError(t, err)
	}

	err = dataprovider.DeleteUser(username, "", "")
	assert.NoError(t, err)
	err = os.RemoveAll(user.GetHomeDir())
	assert.NoError(t, err)
}

func TestQuotaActionsWithQuotaTrackDisabled(t *testing.T) {
	oldProviderConf := dataprovider.GetProviderConfig()
	providerConf := dataprovider.GetProviderConfig()
	providerConf.TrackQuota = 0
	err := dataprovider.Close()
	assert.NoError(t, err)
	err = dataprovider.Initialize(providerConf, configDir, true)
	assert.NoError(t, err)

	username := "u1"
	user := dataprovider.User{
		BaseUser: sdk.BaseUser{
			Username: username,
			HomeDir:  filepath.Join(os.TempDir(), username),
			Status:   1,
			Permissions: map[string][]string{
				"/": {dataprovider.PermAny},
			},
		},
		FsConfig: vfs.Filesystem{
			Provider: sdk.LocalFilesystemProvider,
		},
	}
	err = dataprovider.AddUser(&user, "", "")
	assert.NoError(t, err)

	err = os.MkdirAll(user.GetHomeDir(), os.ModePerm)
	assert.NoError(t, err)
	err = executeRuleAction(dataprovider.BaseEventAction{Type: dataprovider.ActionTypeUserQuotaReset},
		EventParams{}, dataprovider.ConditionOptions{
			Names: []dataprovider.ConditionPattern{
				{
					Pattern: username,
				},
			},
		})
	assert.Error(t, err)

	err = executeRuleAction(dataprovider.BaseEventAction{Type: dataprovider.ActionTypeTransferQuotaReset},
		EventParams{}, dataprovider.ConditionOptions{
			Names: []dataprovider.ConditionPattern{
				{
					Pattern: username,
				},
			},
		})
	assert.Error(t, err)

	err = os.RemoveAll(user.GetHomeDir())
	assert.NoError(t, err)
	err = dataprovider.DeleteUser(username, "", "")
	assert.NoError(t, err)

	foldername := "f1"
	folder := vfs.BaseVirtualFolder{
		Name:       foldername,
		MappedPath: filepath.Join(os.TempDir(), foldername),
	}
	err = dataprovider.AddFolder(&folder, "", "")
	assert.NoError(t, err)
	err = os.MkdirAll(folder.MappedPath, os.ModePerm)
	assert.NoError(t, err)

	err = executeRuleAction(dataprovider.BaseEventAction{Type: dataprovider.ActionTypeFolderQuotaReset},
		EventParams{}, dataprovider.ConditionOptions{
			Names: []dataprovider.ConditionPattern{
				{
					Pattern: foldername,
				},
			},
		})
	assert.Error(t, err)

	err = os.RemoveAll(folder.MappedPath)
	assert.NoError(t, err)
	err = dataprovider.DeleteFolder(foldername, "", "")
	assert.NoError(t, err)

	err = dataprovider.Close()
	assert.NoError(t, err)
	err = dataprovider.Initialize(oldProviderConf, configDir, true)
	assert.NoError(t, err)
}

func TestScheduledActions(t *testing.T) {
	startEventScheduler()
	backupsPath := filepath.Join(os.TempDir(), "backups")
	err := os.RemoveAll(backupsPath)
	assert.NoError(t, err)

	action := &dataprovider.BaseEventAction{
		Name: "action",
		Type: dataprovider.ActionTypeBackup,
	}
	err = dataprovider.AddEventAction(action, "", "")
	assert.NoError(t, err)
	rule := &dataprovider.EventRule{
		Name:    "rule",
		Trigger: dataprovider.EventTriggerSchedule,
		Conditions: dataprovider.EventConditions{
			Schedules: []dataprovider.Schedule{
				{
					Hours:      "11",
					DayOfWeek:  "*",
					DayOfMonth: "*",
					Month:      "*",
				},
			},
		},
		Actions: []dataprovider.EventAction{
			{
				BaseEventAction: dataprovider.BaseEventAction{
					Name: action.Name,
				},
				Order: 1,
			},
		},
	}

	job := eventCronJob{
		ruleName: rule.Name,
	}
	job.Run() // rule not found
	assert.NoDirExists(t, backupsPath)

	err = dataprovider.AddEventRule(rule, "", "")
	assert.NoError(t, err)

	job.Run()
	assert.DirExists(t, backupsPath)

	action.Type = dataprovider.ActionTypeFilesystem
	action.Options = dataprovider.BaseEventActionOptions{
		FsConfig: dataprovider.EventActionFilesystemConfig{
			Type:   dataprovider.FilesystemActionMkdirs,
			MkDirs: []string{"/dir"},
		},
	}
	err = dataprovider.UpdateEventAction(action, "", "")
	assert.NoError(t, err)
	job.Run() // action is not compatible with a scheduled rule

	err = dataprovider.DeleteEventRule(rule.Name, "", "")
	assert.NoError(t, err)
	err = dataprovider.DeleteEventAction(action.Name, "", "")
	assert.NoError(t, err)
	err = os.RemoveAll(backupsPath)
	assert.NoError(t, err)
	stopEventScheduler()
}
