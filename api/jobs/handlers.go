package jobs

import (
	"github.com/emicklei/go-restful"
    	"github.com/haveatry/She-Ra/configdata"
    	"log"
    	"strconv"
	"net/http"
)

type JobManager struct {
    //configMap  map[string]*properties.Properties
    JobMap     map[string]*configdata.Job
    //accessLock *sync.RWMutex
}

type Resource struct {
    JobMng	*JobManager
}

func (d *JobManager) createJob(request *restful.Request, response *restful.Response) {
	jober := new(configdata.Job)
	err := request.ReadEntity(jober)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	jober.Id = strconv.Itoa(len(d.JobMap) + 1)
	// map key id:name after 2016/10
	d.JobMap[jober.Id] = jober	
	log.Print("jober.Id: ", jober.Id)
	response.WriteHeaderAndEntity(http.StatusCreated, jober)
}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {

}

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {

}
