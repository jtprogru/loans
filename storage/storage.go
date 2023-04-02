package storage

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/jtprogru/loans/models"
)

const JSONFile = "data.json"

// StorageInterface определяет интерфейс для работы с данными
type StorageInterface interface {
	SaveData(data interface{}) error
	LoadData(data interface{}) error
}

// JSONStorage структура для работы с JSON файлами
type JSONStorage struct {
	filename string
}

// NewStorage создает экземпляр нужного типа хранилища
func NewStorage(storageType string) (StorageInterface, error) {
	switch storageType {
	case "JSON":
		return &JSONStorage{filename: JSONFile}, nil
	default:
		return nil, errors.New("unsupported storage type")
	}
}

// SaveData сохраняет данные в JSON файл
func (js *JSONStorage) SaveData(data interface{}) error {
	file, err := os.Create(js.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	debtorList, ok := data.(*models.DebtorList)
	if !ok {
		return errors.New("invalid data type for JSON storage")
	}

	encoder := json.NewEncoder(file)
	err = encoder.Encode(debtorList)
	if err != nil {
		return err
	}

	return nil
}

// LoadData загружает данные из JSON файла
func (js *JSONStorage) LoadData(data interface{}) error {
	file, err := os.Open(js.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	debtorList, ok := data.(*models.DebtorList)
	if !ok {
		return errors.New("invalid data type for JSON storage")
	}

	decoder := json.NewDecoder(file)
	err = decoder.Decode(debtorList)
	if err != nil {
		return err
	}

	return nil
}
