//+build extras

package tools

import (
	"context"
	"github.com/Velocidex/ordereddict"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
	"www.velocidex.com/golang/velociraptor/file_store/api"
	"www.velocidex.com/golang/velociraptor/glob"
	vql_subsystem "www.velocidex.com/golang/velociraptor/vql"
	"www.velocidex.com/golang/vfilter"
)

type NextCloudUploadArgs struct {
	File              string `vfilter:"required,field=file,doc=The file to upload"`
	Name              string `vfilter:"optional,field=name,doc=The name of the file that should be stored on the server"`
	Accessor          string `vfilter:"optional,field=accessor,doc=The accessor to use"`
	Url               string `vfilter:"required,field=url,doc=The NextCloud WebDAV url (typically https://<host>/public.php/webdav/)"`
	ShareKey          string `vfilter:"required,field=share_key,doc=The folder upload key, found after /s/ in share links"`
}

type NextCloudUploadFunction struct{}

func (self *NextCloudUploadFunction) Call(ctx context.Context,
	scope *vfilter.Scope,
	args *ordereddict.Dict) vfilter.Any {

	arg := &NextCloudUploadArgs{}
	err := vfilter.ExtractArgs(scope, args, arg)
	if err != nil {
		scope.Log("upload_nextcloud: %s", err.Error())
		return vfilter.Null{}
	}

	err = vql_subsystem.CheckFilesystemAccess(scope, arg.Accessor)
	if err != nil {
		scope.Log("upload_nextcloud: %s", err)
		return vfilter.Null{}
	}

	accessor, err := glob.GetAccessor(arg.Accessor, scope)
	if err != nil {
		scope.Log("upload_nextcloud: %v", err)
		return vfilter.Null{}
	}

	file, err := accessor.Open(arg.File)
	if err != nil {
		scope.Log("upload_nextcloud: Unable to open %s: %s",
			arg.File, err.Error())
		return &vfilter.Null{}
	}
	defer file.Close()

	if arg.Name == "" {
		arg.Name = arg.File
	}

	stat, err := file.Stat()
	if err != nil {
		scope.Log("upload_nextcloud: Unable to stat %s: %v",
			arg.File, err)
	} else if !stat.IsDir() {
		// Abort uploading when the scope is destroyed.
		sub_ctx, cancel := context.WithCancel(ctx)
		scope.AddDestructor(func() {
			cancel()
		})

		upload_response, err := upload_nextcloud(
			sub_ctx, scope, file, stat.Size(),
			arg.Name, arg.Url, arg.ShareKey)
		if err != nil {
			scope.Log("upload_nextcloud: %v", err)
			return vfilter.Null{}
		}
		return upload_response
	}

	return vfilter.Null{}
}

func upload_nextcloud(ctx context.Context, scope *vfilter.Scope,
	reader io.Reader, contentLength int64,
	name string, webdavUrl string, shareKey string) (
	*api.UploadResponse, error) {

	scope.Log("upload_nextcloud: Uploading %v to %v", name, webdavUrl)

	parsedUrl, err := url.Parse(webdavUrl)
	if err != nil {
		return &api.UploadResponse{
			Error: err.Error(),
		}, err
	}
	parsedUrl.Path = path.Join(parsedUrl.Path, name)

	client := &http.Client{
		Timeout: time.Second * 30,
	}

	req, err := http.NewRequest(http.MethodPut, parsedUrl.String(), reader)
	if err != nil {
		return &api.UploadResponse{
			Error: err.Error(),
		}, err
	}

	req.ContentLength = contentLength
	req.SetBasicAuth(shareKey, "")

	resp, err := client.Do(req)
	if err != nil {
		return &api.UploadResponse{
			Error: err.Error(),
		}, err
	}

	scope.Log("upload_nextcloud: HTTP status %v", resp.StatusCode)

	return &api.UploadResponse{
		Path: name,
		Size: uint64(contentLength),
	}, nil
}

func (self NextCloudUploadFunction) Info(
	scope *vfilter.Scope, type_map *vfilter.TypeMap) *vfilter.FunctionInfo {
	return &vfilter.FunctionInfo{
		Name:    "upload_nextcloud",
		Doc:     "Upload files to a NextCloud instance.",
		ArgType: type_map.AddType(scope, &NextCloudUploadArgs{}),
	}
}

func init() {
	vql_subsystem.RegisterFunction(&NextCloudUploadFunction{})
}
