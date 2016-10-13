import (
    "errors"
    "github.com/magiconair/properties"
    "github.com/haveatry/She-Ra/configdata"
    "log"
    "strconv"
    "strings"
    "sync"
    "time"
)

type JobManager struct {
    //configMap  map[string]*properties.Properties
    jobMap     map[string]*configdata.Job
    //accessLock *sync.RWMutex
}

type Resource struct {
    JobMng	*JobManager
}
