package xingchenpan

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "mime/multipart"
    "net/http"
    "strconv"
    "time"

    "github.com/OpenListTeam/OpenList/v4/internal/driver"
    "github.com/OpenListTeam/OpenList/v4/internal/errs"
    "github.com/OpenListTeam/OpenList/v4/internal/model"
    "github.com/OpenListTeam/OpenList/v4/internal/stream"
    "github.com/OpenListTeam/OpenList/v4/pkg/utils"
    "github.com/go-resty/resty/v2"
)

// CZK 结构体实现了 driver.Driver 接口
type CZK struct {
    model.Storage
    Addition
    AccessToken  string
    RefreshToken string
    ExpiresAt    time.Time
    client       *resty.Client
}

// AuthResp 认证接口的响应结构
type AuthResp struct {
    Status  int    `json:"status"`
    Message string `json:"message"`
    Data    struct {
        AccessToken  string `json:"access_token"`
        RefreshToken string `json:"refresh_token"`
        ExpiresIn    int    `json:"expires_in"`
        TokenType    string `json:"token_type"`
    } `json:"data"`
}

// RefreshResp 刷新令牌接口的响应结构
type RefreshResp struct {
    Status  int    `json:"status"`
    Message string `json:"message"`
    Data    struct {
        AccessToken string `json:"access_token"`
        ExpiresIn   int    `json:"expires_in"`
        TokenType   string `json:"token_type"`
    } `json:"data"`
}

// Config 返回驱动配置
func (d *CZK) Config() driver.Config {
    return config
}

// GetAddition 返回驱动的附加配置
func (d *CZK) GetAddition() driver.Additional {
    return &d.Addition
}

// Init 初始化驱动，进行认证
func (d *CZK) Init(ctx context.Context) error {
    d.client = resty.New()
    d.client.SetTimeout(30 * time.Second)
    // 设置全局User-Agent
    d.client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
    return d.authenticate()
}

// Drop 释放资源
func (d *CZK) Drop(ctx context.Context) error {
    return nil
}

// List 列出指定目录下的文件和文件夹
func (d *CZK) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    folderID := dir.GetID()
    url := fmt.Sprintf("%sczkapi/list_files?folder_id=%s", d.baseURL(), folderID)
    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        Get(url)

    if err != nil {
        return nil, fmt.Errorf("failed to send list request: %w", err)
    }

    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to list files with status %d: %s", resp.StatusCode(), resp.String())
    }

    var listResp map[string]interface{}
    if err := json.Unmarshal(resp.Body(), &listResp); err != nil {
        log.Printf("CZK List: failed to parse response: %v, body: %s", err, string(resp.Body()))
        return nil, fmt.Errorf("failed to parse list response: %w", err)
    }

    if code, ok := listResp["code"].(float64); ok && int(code) != 200 {
        message := "unknown error"
        if msg, ok := listResp["message"].(string); ok {
            message = msg
        }
        return nil, fmt.Errorf("list files API error: code=%d, message=%s", int(code), message)
    }

    var objs []model.Obj
    if data, ok := listResp["data"].(map[string]interface{}); ok {
        if items, ok := data["items"].([]interface{}); ok {
            for _, itemData := range items {
                if itemMap, ok := itemData.(map[string]interface{}); ok {
                    obj := &model.Object{
                        ID:       fmt.Sprintf("%.0f", itemMap["id"].(float64)),
                        Name:     itemMap["name"].(string),
                        Modified: time.Now(), // API未提供精确时间，使用当前时间
                        IsFolder: itemMap["type"].(string) == "folder",
                    }
                    if size, ok := itemMap["size"].(float64); ok {
                        obj.Size = int64(size)
                    }
                    objs = append(objs, obj)
                }
            }
        }
    }
    return objs, nil
}

// Link 获取文件的下载链接
func (d *CZK) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    fileID := file.GetID()
    url := fmt.Sprintf("%sczkapi/get_download_url?file_id=%s", d.baseURL(), fileID)
    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        Get(url)

    if err != nil {
        return nil, fmt.Errorf("failed to get download url: %w", err)
    }

    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to get download url with status %d: %s", resp.StatusCode(), resp.String())
    }

    var linkResp map[string]interface{}
    if err := json.Unmarshal(resp.Body(), &linkResp); err != nil {
        return nil, fmt.Errorf("failed to parse download url response: %w", err)
    }

    if data, ok := linkResp["data"].(map[string]interface{}); ok {
        if downloadURL, ok := data["download_url"].(string); ok && downloadURL != "" {
            return &model.Link{URL: downloadURL}, nil
        }
    }
    return nil, fmt.Errorf("download_url not found in response")
}

// Put 上传文件
func (d *CZK) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    // 1. 计算文件MD5
    // 修正：使用 utils.MD5 作为哈希算法，并移除 tempFile.Close()
    tempFile, md5Hash, err := stream.CacheFullAndHash(file, &up, utils.MD5)
    if err != nil {
        return nil, fmt.Errorf("failed to calculate file md5: %w", err)
    }

    // 2. 初始化上传
    initURL := fmt.Sprintf("%sczkapi/first_upload", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("hash", md5Hash)
    _ = writer.WriteField("filename", file.GetName())
    _ = writer.WriteField("filesize", strconv.FormatInt(file.GetSize(), 10))
    _ = writer.WriteField("folder", dstDir.GetID())
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(initURL)

    if err != nil {
        return nil, fmt.Errorf("failed to init upload: %w", err)
    }

    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to init upload with status %d: %s", resp.StatusCode(), resp.String())
    }

    var initResp map[string]interface{}
    if err := json.Unmarshal(resp.Body(), &initResp); err != nil {
        return nil, fmt.Errorf("failed to parse init upload response: %w", err)
    }

    if data, ok := initResp["data"].(map[string]interface{}); ok {
        if status, ok := data["status"].(string); ok && status == "instant" {
            fileID := fmt.Sprintf("%.0f", data["file_id"].(float64))
            return &model.Object{ID: fileID, Name: file.GetName(), Size: file.GetSize(), IsFolder: false}, nil
        }

        uploadURL := data["upload_url"].(string)
        csrfToken := data["csrf_token"].(string)
        fileKey := data["file_key"].(string)

        // 3. 上传文件到存储服务
        if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
            return nil, fmt.Errorf("failed to seek file: %w", err)
        }
        uploadResp, err := d.client.R().SetBody(tempFile).Put(uploadURL)
        if err != nil {
            return nil, fmt.Errorf("failed to upload file to storage: %w", err)
        }
        if uploadResp.StatusCode() != http.StatusOK {
            return nil, fmt.Errorf("failed to upload to storage with status %d", uploadResp.StatusCode())
        }

        // 4. 通知上传完成
        completeURL := fmt.Sprintf("%sczkapi/ok_upload", d.baseURL())
        completePayload := &bytes.Buffer{}
        completeWriter := multipart.NewWriter(completePayload)
        _ = completeWriter.WriteField("hash", md5Hash)
        _ = completeWriter.WriteField("filename", file.GetName())
        _ = completeWriter.WriteField("filesize", strconv.FormatInt(file.GetSize(), 10))
        _ = completeWriter.WriteField("csrf_token", csrfToken)
        _ = completeWriter.WriteField("file_key", fileKey)
        _ = completeWriter.WriteField("folder", dstDir.GetID())
        completeWriter.Close()

        completeResp, err := d.client.R().
            SetHeader("Authorization", "Bearer "+d.AccessToken).
            SetHeader("Content-Type", completeWriter.FormDataContentType()).
            SetBody(completePayload.Bytes()).
            Post(completeURL)

        if err != nil {
            return nil, fmt.Errorf("failed to complete upload: %w", err)
        }
        if completeResp.StatusCode() != http.StatusOK {
            return nil, fmt.Errorf("failed to complete upload with status %d", completeResp.StatusCode())
        }

        var completeRespData map[string]interface{}
        if err := json.Unmarshal(completeResp.Body(), &completeRespData); err != nil {
            return nil, fmt.Errorf("failed to parse complete upload response: %w", err)
        }

        if cData, ok := completeRespData["data"].(map[string]interface{}); ok {
            fileID := fmt.Sprintf("%.0f", cData["file_id"].(float64))
            return &model.Object{ID: fileID, Name: file.GetName(), Size: file.GetSize(), IsFolder: false}, nil
        }
    }
    return nil, fmt.Errorf("failed to parse data from init upload response")
}

// MakeDir 创建目录
func (d *CZK) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    url := fmt.Sprintf("%sczkapi/create_folder", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("parent_id", parentDir.GetID())
    _ = writer.WriteField("name", dirName)
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(url)

    if err != nil {
        return nil, fmt.Errorf("failed to send mkdir request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to create folder with status %d: %s", resp.StatusCode(), resp.String())
    }

    var opResp map[string]interface{}
    if err := json.Unmarshal(resp.Body(), &opResp); err != nil {
        return nil, fmt.Errorf("failed to parse mkdir response: %w", err)
    }
    if data, ok := opResp["data"].(map[string]interface{}); ok {
        folderID := fmt.Sprintf("%.0f", data["folder_id"].(float64))
        return &model.Object{ID: folderID, Name: dirName, IsFolder: true}, nil
    }
    return nil, fmt.Errorf("failed to get folder_id from response")
}

// Move 移动文件或目录
func (d *CZK) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    url := fmt.Sprintf("%sczkapi/move_item", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("id", srcObj.GetID())
    itemType := "folder"
    if !srcObj.IsDir() {
        itemType = "file"
    }
    _ = writer.WriteField("type", itemType)
    _ = writer.WriteField("target_id", dstDir.GetID())
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(url)

    if err != nil {
        return nil, fmt.Errorf("failed to send move request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to move item with status %d: %s", resp.StatusCode(), resp.String())
    }
    // 返回更新后的对象
    return &model.Object{
        ID:       srcObj.GetID(),
        Name:     srcObj.GetName(),
        Size:     srcObj.GetSize(),
        Modified: time.Now(),
        IsFolder: srcObj.IsDir(),
    }, nil
}

// Rename 重命名文件或目录
func (d *CZK) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return nil, fmt.Errorf("failed to refresh token: %w", err)
    }

    url := fmt.Sprintf("%sczkapi/rename_item", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("id", srcObj.GetID())
    itemType := "folder"
    if !srcObj.IsDir() {
        itemType = "file"
    }
    _ = writer.WriteField("type", itemType)
    _ = writer.WriteField("new_name", newName)
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(url)

    if err != nil {
        return nil, fmt.Errorf("failed to send rename request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return nil, fmt.Errorf("failed to rename item with status %d: %s", resp.StatusCode(), resp.String())
    }
    return &model.Object{
        ID:       srcObj.GetID(),
        Name:     newName,
        Size:     srcObj.GetSize(),
        Modified: time.Now(),
        IsFolder: srcObj.IsDir(),
    }, nil
}

// Remove 删除文件或目录
func (d *CZK) Remove(ctx context.Context, obj model.Obj) error {
    if err := d.refreshTokenIfNeeded(); err != nil {
        return fmt.Errorf("failed to refresh token: %w", err)
    }

    url := fmt.Sprintf("%sczkapi/delete_item", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("id", obj.GetID())
    itemType := "folder"
    if !obj.IsDir() {
        itemType = "file"
    }
    _ = writer.WriteField("type", itemType)
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Authorization", "Bearer "+d.AccessToken).
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(url)

    if err != nil {
        return fmt.Errorf("failed to send delete request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return fmt.Errorf("failed to delete item with status %d: %s", resp.StatusCode(), resp.String())
    }
    return nil
}

// --- 认证和辅助方法 ---

// baseURL 获取API基础URL
func (d *CZK) baseURL() string {
    return "https://pan.szczk.top/"
}

// authenticate 进行初始认证
func (d *CZK) authenticate() error {
    log.Println("CZK: authenticating...")
    url := fmt.Sprintf("%sczkapi/authenticate", d.baseURL())
    if d.APIKey == "" || d.APISecret == "" {
        return fmt.Errorf("API key or secret not set in addition config")
    }

    resp, err := d.client.R().
        SetHeader("x-api-key", d.APIKey).
        SetHeader("x-api-secret", d.APISecret).
        Get(url)

    if err != nil {
        return fmt.Errorf("failed to send auth request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return fmt.Errorf("authentication failed with status %d: %s", resp.StatusCode(), resp.String())
    }

    var authResp AuthResp
    if err := json.Unmarshal(resp.Body(), &authResp); err != nil {
        return fmt.Errorf("failed to parse auth response: %w", err)
    }

    if authResp.Status != 200 {
        return fmt.Errorf("authentication API error: status=%d, message=%s", authResp.Status, authResp.Message)
    }

    d.AccessToken = authResp.Data.AccessToken
    d.RefreshToken = authResp.Data.RefreshToken
    d.ExpiresAt = time.Now().Add(time.Duration(authResp.Data.ExpiresIn) * time.Second)
    log.Println("CZK: authentication successful")
    return nil
}

// refreshToken 刷新访问令牌
func (d *CZK) refreshToken() error {
    log.Println("CZK: refreshing token...")
    if d.RefreshToken == "" {
        return fmt.Errorf("no refresh token available")
    }

    url := fmt.Sprintf("%sczkapi/refresh_token", d.baseURL())
    payload := &bytes.Buffer{}
    writer := multipart.NewWriter(payload)
    _ = writer.WriteField("refresh_token", d.RefreshToken)
    writer.Close()

    resp, err := d.client.R().
        SetHeader("Content-Type", writer.FormDataContentType()).
        SetBody(payload.Bytes()).
        Post(url)

    if err != nil {
        return fmt.Errorf("failed to send refresh request: %w", err)
    }
    if resp.StatusCode() != http.StatusOK {
        return fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode(), resp.String())
    }

    var refreshResp RefreshResp
    if err := json.Unmarshal(resp.Body(), &refreshResp); err != nil {
        return fmt.Errorf("failed to parse refresh response: %w", err)
    }

    if refreshResp.Status != 200 {
        return fmt.Errorf("token refresh API error: status=%d, message=%s", refreshResp.Status, refreshResp.Message)
    }

    d.AccessToken = refreshResp.Data.AccessToken
    d.ExpiresAt = time.Now().Add(time.Duration(refreshResp.Data.ExpiresIn) * time.Second)
    log.Println("CZK: token refreshed successfully")
    return nil
}

// refreshTokenIfNeeded 检查并在需要时刷新令牌
func (d *CZK) refreshTokenIfNeeded() error {
    if time.Now().After(d.ExpiresAt) {
        log.Println("CZK: token expired, refreshing...")
        return d.refreshToken()
    }
    return nil
}

// --- 未实现或不支持的方法 ---

// Copy 被禁用，因为星辰云盘API不支持复制操作
func (d *CZK) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
    // 明确返回 NotSupport 错误，以便UI可以禁用复制功能
    return nil, errs.NotSupport
}

func (d *CZK) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
    return nil, errs.NotImplement
}

func (d *CZK) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
    return nil, errs.NotImplement
}

func (d *CZK) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
    return nil, errs.NotImplement
}

func (d *CZK) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
    return nil, errs.NotImplement
}

func (d *CZK) GetDetails(ctx context.Context) (*model.StorageDetails, error) {
    return nil, errs.NotImplement
}

// 确保CZK结构体实现了driver.Driver接口
var _ driver.Driver = (*CZK)(nil)
