package redis

import (
	"strings"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixApiKeys = "apikey:"
)

func dbToApiKey(id string, input []any) (models.ApiKey, error) {
	var result models.ApiKey
	err := redigo.ScanStruct(input, &result)
	result.Id = strings.Replace(id, prefixApiKeys, "", 1)
	return result, err
}

// GetAllApiKeys returns a map with all API keys
func (p DatabaseProvider) GetAllApiKeys() map[string]models.ApiKey {
	result := make(map[string]models.ApiKey)
	maps := p.getAllHashesWithPrefix(prefixApiKeys)
	for k, v := range maps {
		apiKey, err := dbToApiKey(k, v)
		helper.Check(err)
		result[apiKey.Id] = apiKey
	}
	return result
}

// GetApiKey returns a models.ApiKey if valid or false if the ID is not valid
func (p DatabaseProvider) GetApiKey(id string) (models.ApiKey, bool) {
	result, ok := p.getHashMap(prefixApiKeys + id)
	if !ok {
		return models.ApiKey{}, false
	}
	apikey, err := dbToApiKey(id, result)
	helper.Check(err)
	return apikey, true
}

// GetApiKeyByPublicKey returns an API key by using the public key
func (p DatabaseProvider) GetApiKeyByPublicKey(publicKey string) (string, bool) {
	keys := p.GetAllApiKeys()
	for _, key := range keys {
		if key.PublicId == publicKey {
			return key.Id, true
		}
	}
	return "", false
}

// SaveApiKey saves the API key to the database
func (p DatabaseProvider) SaveApiKey(apikey models.ApiKey) {
	p.setHashMap(p.buildArgs(prefixApiKeys + apikey.Id).AddFlat(apikey))
	if apikey.Expiry != 0 {
		p.setExpiryAt(prefixApiKeys+apikey.Id, apikey.Expiry)
	}
}

var insertApiKey = redigo.NewScript(1, `
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
redis.call('HSET', KEYS[1], unpack(ARGV, 2))
if tonumber(ARGV[1]) ~= 0 then redis.call('EXPIREAT', KEYS[1], ARGV[1]) end
return 1
`)

// InsertApiKey checks and inserts in one Redis operation, including across instances.
func (p DatabaseProvider) InsertApiKey(apikey models.ApiKey) (bool, error) {
	conn := p.pool.Get()
	defer conn.Close()
	args := redigo.Args{}.Add(p.dbPrefix + prefixApiKeys + apikey.Id).Add(apikey.Expiry).AddFlat(apikey)
	count, err := redigo.Int(insertApiKey.Do(conn, args...))
	return count == 1, err
}

// UpdateTimeApiKey writes the content of LastUsage to the database
func (p DatabaseProvider) UpdateTimeApiKey(apikey models.ApiKey) {
	p.updateUsage(prefixApiKeys+apikey.Id, "LastUsed", apikey.LastUsed)
}

// DeleteApiKey deletes an API key with the given ID
func (p DatabaseProvider) DeleteApiKey(id string) {
	p.deleteKey(prefixApiKeys + id)
}
