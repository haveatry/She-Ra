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

func (d *Resource) createJob(request *restful.Request, response *restful.Response) {

}

func (d *Resource) findJob(request *restful.Request, response *restful.Response) {

}

func (d *Resource) updateJob(request *restful.Request, response *restful.Response) {

}
