package redis

import (
	"bytes"
	"crypto/rand"
	"encoding/gob"
	"strings"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixMetaData = "fmeta:"
)

// GetAllMetadata returns a map of all available files
func (p DatabaseProvider) GetAllMetadata() map[string]models.File {
	result := make(map[string]models.File)
	maps := p.getAllHashesWithPrefix(prefixMetaData)
	for k, v := range maps {
		file, err := dbToMetadata(k, v)
		helper.Check(err)
		result[file.Id] = file
	}
	return result
}

func dbToMetadata(id string, input []any) (models.File, error) {
	var result models.File
	err := redigo.ScanStruct(input, &result)
	if err != nil {
		return models.File{}, err
	}
	result.Id = strings.Replace(id, prefixMetaData, "", 1)
	return unmarshalEncryptionInfo(result)
}

func marshalEncryptionInfo(f models.File) (models.File, error) {
	var encInfo bytes.Buffer
	enc := gob.NewEncoder(&encInfo)
	err := enc.Encode(f.Encryption)
	if err != nil {
		return f, err
	}
	f.InternalRedisEncryption = encInfo.Bytes()
	return f, nil
}

func unmarshalEncryptionInfo(f models.File) (models.File, error) {
	if f.InternalRedisEncryption == nil {
		f.Encryption = models.EncryptionInfo{}
		return f, nil
	}
	var result models.EncryptionInfo
	buf := bytes.NewBuffer(f.InternalRedisEncryption)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&result)
	if err != nil {
		return f, err
	}
	f.Encryption = result
	f.InternalRedisEncryption = nil
	return f, nil
}

// GetMetaDataById returns a models.File from the ID passed or false if the id is not valid
func (p DatabaseProvider) GetMetaDataById(id string) (models.File, bool) {
	result, ok := p.getHashMap(prefixMetaData + id)
	if !ok {
		return models.File{}, false
	}
	file, err := dbToMetadata(id, result)
	helper.Check(err)
	return file, true
}

// SaveMetaData stores the metadata of a file to the disk
func (p DatabaseProvider) SaveMetaData(file models.File) {
	marshalledFile, err := marshalEncryptionInfo(file)
	helper.Check(err)
	args := redigo.Args{}.Add(p.dbPrefix+prefixMetaData+file.Id, p.dbPrefix+keyMetadataRevision, rand.Text(), "save").AddFlat(marshalledFile)
	conn := p.pool.Get()
	defer conn.Close()
	_, err = mutateMetadataWithRevision.Do(conn, args...)
	helper.Check(err)
}

// DeleteMetaData deletes information about a file
func (p DatabaseProvider) DeleteMetaData(id string) {
	conn := p.pool.Get()
	defer conn.Close()
	_, err := mutateMetadataWithRevision.Do(conn, p.dbPrefix+prefixMetaData+id, p.dbPrefix+keyMetadataRevision, rand.Text(), "delete")
	helper.Check(err)
}

// Only metadata notification uses Lua; quota arithmetic remains checked int64
// in Go. Change the opaque revision first. If a later write fails, WATCH users
// conservatively retry; metadata can never change with an unchanged revision.
var mutateMetadataWithRevision = redigo.NewScript(2, `
local revisionType = redis.call('TYPE', KEYS[2]).ok
if revisionType ~= 'none' and revisionType ~= 'string' then
    return redis.error_reply('invalid metadata revision type')
end
if ARGV[2] == 'save' then
    local metadataType = redis.call('TYPE', KEYS[1]).ok
    if metadataType ~= 'none' and metadataType ~= 'hash' then
        return redis.error_reply('invalid metadata type')
    end
end
redis.call('SET', KEYS[2], ARGV[1])
if ARGV[2] == 'delete' then
    return redis.call('DEL', KEYS[1])
end
return redis.call('HMSET', KEYS[1], unpack(ARGV, 3))
`)

// IncreaseDownloadCount increases the download count of a file atomically
func (p DatabaseProvider) IncreaseDownloadCount(id string, decreaseRemainingDownloads bool) {
	if decreaseRemainingDownloads {
		p.decreaseHashmapIntField(prefixMetaData+id, "DownloadsRemaining")
	}
	p.increaseHashmapIntField(prefixMetaData+id, "DownloadCount")
}

// GetDownloadsRemaining returns the remaining downloads of a file that does not implement UnlimitedDownloads
func (p DatabaseProvider) GetDownloadsRemaining(id string) int {
	file, ok := p.GetMetaDataById(id)
	if !ok {
		return 0
	}
	return file.DownloadsRemaining
}
