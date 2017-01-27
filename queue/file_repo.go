package queue

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"sync"
)

type fileRepository struct {
	rootPath string

	mux  sync.Mutex
	dict map[string]*sync.RWMutex
}

const queueRepoDir = "/queue"

func newFileRepository(path string) *fileRepository {
	if path == "" {
		log.Println("error: `path` must not be empty string")
		return nil
	}

	root, err := filepath.Abs(path + queueRepoDir)
	if err != nil {
		log.Printf("error: cannot get the path to the queue storage: %v\n", err)
		return nil
	}

	if !exists(root) {
		if err := os.MkdirAll(root, os.ModePerm); err != nil {
			log.Printf("error: cannot create the queue dir: %v\n", err)
			return nil
		}
	}

	return &fileRepository{
		rootPath: root,
		mux:      sync.Mutex{},
		dict:     make(map[string]*sync.RWMutex),
	}
}

func (s *fileRepository) validatePath(owner string, name string) bool {
	ok, _ := createQueueJSONPath(s.rootPath, owner, name)
	return ok
}

func (s *fileRepository) getPerFileLock(owner, name string) *sync.RWMutex {
	s.mux.Lock()
	defer s.mux.Unlock()

	k := owner + "/" + name
	mux, ok := s.dict[k]
	if !ok {
		v := new(sync.RWMutex)
		s.dict[k] = v
		mux = v
	}

	return mux
}

func (s *fileRepository) save(owner string, name string, queue *AutoMergeQueue) bool {
	ok, file := createQueueJSONPath(s.rootPath, owner, name)
	if !ok {
		return false
	}

	b := encodeAutoMergeQueueToByte(queue)
	if b == nil {
		fmt.Println("error: cannot marshal queue")
		return false
	}

	mux := s.getPerFileLock(owner, name)
	mux.Lock()
	defer mux.Unlock()

	{
		dir := path.Dir(file)
		if !exists(dir) {
			if err := os.Mkdir(dir, 0775); err != nil {
				log.Println("error: cannot create the config home dir.")
				return false
			}
		}
	}

	// If the file exists, rename the current file as `***.bak` file.
	var back string
	if exists(file) {
		back = file + ".old"
		if err := os.Rename(file, back); err != nil {
			panic(err)
		}
	}
	// clean up the backup file after all.
	defer (func(p string) {
		if p == "" {
			return
		}

		if err := os.Remove(p); err != nil {
			log.Printf("error: cannot clean up the backup file: %v\n", p)
		}
	})(back)

	if err := ioutil.WriteFile(file, b, 0644); err != nil {
		fmt.Printf("error: cannot write the data to %v: %v\n", file, err)
		return false
	}

	return true
}

func (s *fileRepository) load(owner string, name string) (bool, *AutoMergeQueue) {
	b := s.readMergeQueue(owner, name)
	if b == nil {
		return false, nil
	}

	q := decodeByteToAutoMergeQueue(b)
	return true, q
}

func (s *fileRepository) loadAsByte(owner string, name string) (bool, []byte) {
	b := s.readMergeQueue(owner, name)
	if b == nil {
		return false, nil
	}

	return true, b
}

func (s *fileRepository) readMergeQueue(owner, name string) []byte {
	ok, file := createQueueJSONPath(s.rootPath, owner, name)
	if !ok {
		return nil
	}

	mux := s.getPerFileLock(owner, name)
	mux.RLock()
	defer mux.RUnlock()

	if !exists(file) {
		return nil
	}

	b, err := ioutil.ReadFile(file)
	if err != nil {
		log.Println("error: cannot read the file")
		return nil
	}

	return b
}

func exists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func createQueueJSONPath(root, owner, name string) (ok bool, path string) {
	reponame := owner + "/" + name + ".json"
	file, err := createAbs(root, reponame)
	if err != nil {
		log.Printf("error: %v\n", err)
		return false, ""
	}

	return true, file
}

// Check `p` is insecure string as a path.
// If `p` is `../`, it can access to security path (e.g. `~/.ssh/`).
func validPathFragment(p string) bool {
	if path.Base(p) == p {
		return true
	}

	return false
}

// XXX: Update this field when change the data struct.
const fileFmtVersion int32 = 0

type autoMergeQFile struct {
	Version int32 `json:"version"`
	Auto    struct {
		Queue   []*AutoMergeQueueItem `json:"queue"`
		Current *AutoMergeQueueItem   `json:"current_active"`
	} `json:"auto_merge"`
}

func decodeByteToAutoMergeQueue(b []byte) *AutoMergeQueue {
	var result autoMergeQFile
	if err := json.Unmarshal(b, &result); err != nil {
		fmt.Println("error:", err)
		return nil
	}

	q := AutoMergeQueue{
		q:       result.Auto.Queue,
		current: result.Auto.Current,
	}

	return &q
}

func encodeAutoMergeQueueToByte(queue *AutoMergeQueue) []byte {
	c := autoMergeQFile{
		Version: fileFmtVersion,
		Auto: struct {
			Queue   []*AutoMergeQueueItem `json:"queue"`
			Current *AutoMergeQueueItem   `json:"current_active"`
		}{
			Queue:   queue.q,
			Current: queue.current,
		},
	}

	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		fmt.Println("error: cannot marshal queue:", err)
		return nil
	}

	return b
}
