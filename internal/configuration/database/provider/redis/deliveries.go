package redis

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const keyDeliveries = "delivery:records"
const keyDeliveriesVersion = "delivery:version"
const maxDeliveryRecordBytes = 24000

func (p DatabaseProvider) initDeliveries() error {
	conn := p.pool.Get()
	defer conn.Close()
	if _, err := conn.Do("SETNX", p.dbPrefix+keyDeliveriesVersion, 1); err != nil {
		return err
	}
	return p.checkDeliverySchema(context.Background(), conn)
}

func (p DatabaseProvider) checkDeliverySchema(ctx context.Context, conn redigo.Conn) error {
	version, err := redigo.Int(redigo.DoContext(conn, ctx, "GET", p.dbPrefix+keyDeliveriesVersion))
	if errors.Is(err, redigo.ErrNil) {
		// Only a genuinely absent legacy structure is empty. A missing
		// marker beside existing records is corruption, not permission to
		// silently drop delivery capabilities during offline migration.
		exists, readErr := redigo.Bool(redigo.DoContext(conn, ctx, "EXISTS", p.dbPrefix+keyDeliveries))
		if readErr != nil {
			return readErr
		}
		if exists {
			return uploadquota.ErrSchema
		}
		return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
	}
	if err != nil {
		return err
	}
	if version != 1 {
		return uploadquota.ErrSchema
	}
	return nil
}

func (p DatabaseProvider) deliveryConnection(ctx context.Context) (redigo.Conn, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.pool == nil {
		return nil, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.checkDeliverySchema(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func decodeDelivery(encoded []byte, token string) (models.Delivery, error) {
	var d models.Delivery
	if len(encoded) > maxDeliveryRecordBytes || json.Unmarshal(encoded, &d) != nil || !d.Valid() || d.Token != token {
		return models.Delivery{}, uploadquota.ErrInvalid
	}
	return d, nil
}

func (p DatabaseProvider) InsertDelivery(ctx context.Context, d models.Delivery) (bool, error) {
	if !d.Valid() {
		return false, uploadquota.ErrInvalid
	}
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > maxDeliveryRecordBytes {
		return false, uploadquota.ErrInvalid
	}
	return redigo.Bool(redigo.DoContext(conn, ctx, "HSETNX", p.dbPrefix+keyDeliveries, d.Token, encoded))
}

func (p DatabaseProvider) ReadDelivery(ctx context.Context, token string) (models.Delivery, bool, error) {
	if !models.ValidDeliveryToken(token) {
		return models.Delivery{}, false, uploadquota.ErrInvalid
	}
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return models.Delivery{}, false, err
	}
	defer conn.Close()
	encoded, err := redigo.Bytes(redigo.DoContext(conn, ctx, "HGET", p.dbPrefix+keyDeliveries, token))
	if errors.Is(err, redigo.ErrNil) {
		return models.Delivery{}, false, nil
	}
	if err != nil {
		return models.Delivery{}, false, err
	}
	d, err := decodeDelivery(encoded, token)
	return d, err == nil, err
}

func (p DatabaseProvider) ListDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	if ownerID <= 0 {
		return nil, uploadquota.ErrInvalid
	}
	return p.readDeliveries(ctx, ownerID)
}

// ReadAllDeliveries is for an offline migration of every owner's records.
func (p DatabaseProvider) ReadAllDeliveries(ctx context.Context) ([]models.Delivery, error) {
	return p.readDeliveries(ctx, 0)
}

func (p DatabaseProvider) readDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	byToken := make(map[string]models.Delivery)
	cursor := "0"
	for {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "HSCAN", p.dbPrefix+keyDeliveries, cursor, "COUNT", 100))
		if err != nil {
			return nil, err
		}
		var fields [][]byte
		if _, err := redigo.Scan(reply, &cursor, &fields); err != nil || len(fields)%2 != 0 {
			return nil, uploadquota.ErrInvalid
		}
		for i := 0; i < len(fields); i += 2 {
			d, err := decodeDelivery(fields[i+1], string(fields[i]))
			if err != nil {
				return nil, err
			}
			if ownerID == 0 || ownerID == d.OwnerID {
				byToken[d.Token] = d
			}
		}
		if cursor == "0" {
			break
		}
	}
	result := make([]models.Delivery, 0, len(byToken))
	for _, d := range byToken {
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b models.Delivery) int {
		return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.Token, b.Token))
	})
	return result, nil
}

const revokeDeliveryScript = `
local raw = redis.call('HGET', KEYS[1], ARGV[1])
if not raw then return 0 end
local ok, d = pcall(cjson.decode, raw)
if not ok or type(d) ~= 'table' or type(d.ownerId) ~= 'number' or type(d.revokedAt) ~= 'number' then return -1 end
if d.ownerId ~= tonumber(ARGV[2]) then return 0 end
if d.revokedAt == 0 then
  d.revokedAt = tonumber(ARGV[3])
  redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(d))
end
return 1`

func (p DatabaseProvider) RevokeDelivery(ctx context.Context, token string, ownerID int, now int64) (bool, error) {
	if !models.ValidDeliveryToken(token) || ownerID <= 0 || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	result, err := redigo.Int(redigo.DoContext(conn, ctx, "EVAL", revokeDeliveryScript, 1, p.dbPrefix+keyDeliveries, token, ownerID, now))
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, uploadquota.ErrInvalid
	}
	return result == 1, nil
}
