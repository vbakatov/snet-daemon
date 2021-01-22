package escrow

import (
	"github.com/singnet/snet-daemon/storage"
	"strings"
	"sync"
)

type memoryStorage struct {
	data  map[string]string
	mutex *sync.RWMutex
}

// NewMemStorage returns new in-memory atomic storage implementation
func NewMemStorage() (storage *memoryStorage) {
	return &memoryStorage{
		data:  make(map[string]string),
		mutex: &sync.RWMutex{},
	}
}

func (storage *memoryStorage) Put(key, value string) (err error) {
	storage.mutex.Lock()
	defer storage.mutex.Unlock()

	return storage.unsafePut(key, value)
}

func (storage *memoryStorage) unsafePut(key, value string) (err error) {
	storage.data[key] = value
	return nil
}

func (storage *memoryStorage) Get(key string) (value string, ok bool, err error) {
	storage.mutex.RLock()
	defer storage.mutex.RUnlock()

	return storage.unsafeGet(key)
}

func (storage *memoryStorage) GetByKeyPrefix(prefix string) (values []string, err error) {
	storage.mutex.RLock()
	defer storage.mutex.RUnlock()

	for key, value := range storage.data {
		if strings.HasPrefix(key, prefix) {
			values = append(values, value)
		}
	}

	return
}

func (storage *memoryStorage) unsafeGet(key string) (value string, ok bool, err error) {
	value, ok = storage.data[key]
	if !ok {
		return "", false, nil
	}
	return value, true, nil
}

func (storage *memoryStorage) PutIfAbsent(key, value string) (ok bool, err error) {
	storage.mutex.Lock()
	defer storage.mutex.Unlock()

	_, ok, err = storage.unsafeGet(key)
	if err != nil {
		return
	}

	if ok {
		return false, nil
	}

	return true, storage.unsafePut(key, value)
}

func (storage *memoryStorage) CompareAndSwap(key, prevValue, newValue string) (ok bool, err error) {
	storage.mutex.Lock()
	defer storage.mutex.Unlock()

	current, ok, err := storage.unsafeGet(key)
	if err != nil {
		return
	}

	if !ok || current != prevValue {
		return false, nil
	}

	return true, storage.unsafePut(key, newValue)
}

func (storage *memoryStorage) Delete(key string) (err error) {
	storage.mutex.Lock()
	defer storage.mutex.Unlock()

	delete(storage.data, key)

	return
}

func (storage *memoryStorage) Clear() (err error) {
	storage.mutex.Lock()
	defer storage.mutex.Unlock()

	storage.data = make(map[string]string)

	return
}

func (memStorage *memoryStorage) StartTransaction(conditionKeys []string) (transaction storage.Transaction, err error) {
	conditionKeyValues := make([]storage.KeyValueData, len(conditionKeys))
	for i, key := range conditionKeys {
		value, ok, err := memStorage.Get(key)
		if err != nil {
			return nil, err
		} else if !ok {
			conditionKeyValues[i] = storage.KeyValueData{Key: key, Value: "", Present: false}
		} else {
			conditionKeyValues[i] = storage.KeyValueData{Key: key, Value: value, Present: true}
		}

	}
	transaction = &memoryStorageTransaction{ConditionKeys: conditionKeys, ConditionValues: conditionKeyValues}
	return transaction, nil
}

func getValueDataForKey(key string, update []storage.KeyValueData) (data storage.KeyValueData, present bool) {
	for _, data := range update {
		if strings.Compare(data.Key, key) == 0 {
			return data, true
		}
	}
	return data, false
}
func (storage *memoryStorage) CompleteTransaction(transaction storage.Transaction, update []storage.KeyValueData) (ok bool, err error) {
	originalValues := transaction.(*memoryStorageTransaction).ConditionValues
	for _, olddata := range originalValues {
		if olddata.Present {
			//make sure the current value is the same as the value last read
			currentValue, ok, err := storage.Get(olddata.Key)
			if !ok || err != nil {
				return ok, err
			}
			if strings.Compare(currentValue, olddata.Value) == 0 {
				if updatedData, ok := getValueDataForKey(olddata.Key, update); ok {
					if err = storage.Put(updatedData.Key, updatedData.Value); err != nil {
						return false, err
					}
					continue
				}
			}

		} else {
			if updatedData, ok := getValueDataForKey(olddata.Key, update); ok {
				if ok, err := storage.PutIfAbsent(updatedData.Key, updatedData.Value); err != nil {
					return false, err
				} else if !ok {
					return ok, nil
				}
				continue
			}
		}
	}
	return true, nil
}

func (client *memoryStorage) ExecuteTransaction(request storage.CASRequest) (ok bool, err error) {

	transaction, err := client.StartTransaction(request.ConditionKeys)
	if err != nil {
		return false, err
	}
	for {
		oldvalues, err := transaction.GetConditionValues()
		if err != nil {
			return false, err
		}
		newvalues, ok, err := request.Update(oldvalues)
		if err != nil {
			return false, err
		}
		ok, err = client.CompleteTransaction(transaction, newvalues)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
		if request.RetryTillSuccessOrError {
			continue
		}
	}
	return true, nil
}

type memoryStorageTransaction struct {
	ConditionValues []storage.KeyValueData
	ConditionKeys   []string
}

func (transaction *memoryStorageTransaction) GetConditionValues() ([]storage.KeyValueData, error) {
	values := make([]storage.KeyValueData, len(transaction.ConditionValues))
	for i, value := range transaction.ConditionValues {
		values[i] = storage.KeyValueData{
			Key:     value.Key,
			Value:   value.Value,
			Present: value.Present,
		}
	}
	return values, nil
}
