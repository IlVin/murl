package config

import (
	"encoding/json"
	"errors"
	"os"
)

// Чтение конфига БД из JSON формата:
//  {
//      "Clusters": {
//          "ClusterName" : {
//              "Shards": [
//                  { "ShrdID": "00", "RW": ["dsn1", .., "dsnN"], "RO": ["dsn1", .., "dsnN"] },
//                  ...
//                  { "ShrdID": "NN", "RW": ["dsn1", .., "dsnN"], "RO": ["dsn1", .., "dsnN"] },
//              ]
//          }
//      }
//  }

// DBShrdProps проперти шарда
type DBShrdProps struct {
	ShrdID string
	RW     []string
	RO     []string
}

type DBShrdList []DBShrdProps

// DBClusterProps проперти кластера
type DBClusterProps struct {
	Shards DBShrdList
}

// DBClusterConfig конфиг всех кластеров, с которыми работает приложение
type DBClusterConfig struct {
	Clusters map[string]DBClusterProps
}

// Exists возвращает признак существования шарда с указанным суффиксом
func (s DBShrdList) Exists(shrdID string) bool {
	for _, v := range s {
		if v.ShrdID == shrdID {
			return true
		}
	}

	return false
}

func LoadDBConfig(path string) (*DBClusterConfig, error) {
	jsonData, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(ErrCannotReadConfigFile, err)
	}

	dbConfig := DBClusterConfig{}

	err = json.Unmarshal([]byte(jsonData), &dbConfig)
	if err != nil {
		return nil, errors.Join(ErrBadJSONFormat, err)
	}

	return &dbConfig, nil
}
