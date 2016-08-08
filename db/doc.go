/* Document management and index maintenance. */
package db

import (
	"encoding/json"
	"fmt"
	"github.com/davidmcclelland/tiedot/tdlog"
	"math/rand"
)

// Resolve the attribute(s) in the document structure along the given path.
func GetIn(doc interface{}, path []string) (ret []interface{}) {
	docMap, ok := doc.(map[string]interface{})
	if !ok {
		return
	}
	var thing interface{} = docMap
	// Get into each path segment
	for i, seg := range path {
		if aMap, ok := thing.(map[string]interface{}); ok {
			thing = aMap[seg]
		} else if anArray, ok := thing.([]interface{}); ok {
			for _, element := range anArray {
				ret = append(ret, GetIn(element, path[i:])...)
			}
			return ret
		} else {
			return nil
		}
	}
	switch thing.(type) {
	case []interface{}:
		return append(ret, thing.([]interface{})...)
	default:
		return append(ret, thing)
	}
}

// Hash a string using sdbm algorithm.
func StrHash(str string) uint64 {
	var hash uint64
	for _, c := range str {
		hash = uint64(c) + (hash << 6) + (hash << 16) - hash
	}
	return hash
}

// Put a document on all user-created indexes.
func (col *Col) indexDoc(id uint64, doc map[string]interface{}) {
	for idxName, idxPath := range col.indexPaths {
		for _, idxVal := range GetIn(doc, idxPath) {
			if idxVal != nil {
				col.hts[idxName].Put(StrHash(fmt.Sprint(idxVal)), id)
			}
		}
	}
}

// Remove a document from all user-created indexes.
func (col *Col) unindexDoc(id uint64, doc map[string]interface{}) {
	for idxName, idxPath := range col.indexPaths {
		for _, idxVal := range GetIn(doc, idxPath) {
			if idxVal != nil {
				col.hts[idxName].Remove(StrHash(fmt.Sprint(idxVal)), id)
			}
		}
	}
}

// Insert a document with the specified ID into the collection (incl. index).
func (col *Col) insertRecovery(id uint64, doc map[string]interface{}) (err error) {
	docJS, err := json.Marshal(doc)
	if err != nil {
		return
	}
	// Put document data into collection
	if _, err = col.part.Insert(id, []byte(docJS)); err != nil {
		return
	}
	// Index the document
	col.indexDoc(id, doc)
	return
}

// Insert a document into the collection.
func (col *Col) Insert(doc map[string]interface{}) (id uint64, err error) {
	docJS, err := json.Marshal(doc)
	if err != nil {
		return
	}
	id = uint64(rand.Int63())
	col.db.lock.Lock()
	// Put document data into collection
	if _, err = col.part.Insert(id, []byte(docJS)); err != nil {
		col.db.lock.Unlock()
		return
	}
	// Index the document
	col.indexDoc(id, doc)
	col.db.lock.Unlock()
	return
}

func (col *Col) read(id uint64) (doc map[string]interface{}, err error) {
	docB, err := col.part.Read(id)
	if err != nil {
		return
	}
	err = json.Unmarshal(docB, &doc)
	return
}

// Find and retrieve a document by ID.
func (col *Col) Read(id uint64) (doc map[string]interface{}, err error) {
	col.db.lock.RLock()
	doc, err = col.read(id)
	col.db.lock.RUnlock()
	return
}

// Update a document.
func (col *Col) Update(id uint64, doc map[string]interface{}) error {
	if doc == nil {
		return fmt.Errorf("Updating %d: input doc may not be nil", id)
	}
	docJS, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	col.db.lock.Lock()
	originalB, err := col.part.Read(id)
	if err != nil {
		col.db.lock.Unlock()
		return err
	}
	var original map[string]interface{}
	if err = json.Unmarshal(originalB, &original); err != nil {
		tdlog.Noticef("Will not attempt to unindex document %d during update", id)
	}
	if err = col.part.Update(id, []byte(docJS)); err != nil {
		col.db.lock.Unlock()
		return err
	}
	// Done with the collection data, next is to maintain indexed values
	if original != nil {
		col.unindexDoc(id, original)
	}
	col.indexDoc(id, doc)
	col.db.lock.Unlock()
	return nil
}

// Delete a document.
func (col *Col) Delete(id uint64) error {
	col.db.lock.Lock()
	originalB, err := col.part.Read(id)
	if err != nil {
		col.db.lock.Unlock()
		return err
	}
	var original map[string]interface{}
	if err = json.Unmarshal(originalB, &original); err != nil {
		tdlog.Noticef("Will not attempt to unindex document %d during delete", id)
	}
	if err = col.part.Delete(id); err != nil {
		col.db.lock.Unlock()
		return err
	}
	// Done with the collection data, next is to remove indexed values
	if original != nil {
		col.unindexDoc(id, original)
	}
	col.db.lock.Unlock()
	return nil
}
