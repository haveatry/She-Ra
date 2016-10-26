package jobs

import (
	"github.com/emicklei/go-restful"
	//. "github.com/haveatry/She-Ra/api/response"
	//. "github.com/haveatry/She-Ra/configdata"
	. "github.com/jianzi123/She-Ra/api/response"
	. "github.com/jianzi123/She-Ra/configdata"
)

func WebService(jobMng *JobManager) *restful.WebService {
	dc := jobMng
	return dc.WebService()
}



func Register(jobMng *JobManager, container *restful.Container, cors bool) {
	dc := jobMng
	dc.Register(container, cors)
}

func (d JobManager) Register(container *restful.Container, cors bool) {
	ws := d.WebService()

	// Cross Origin Resource Sharing filter
	if cors {
		corsRule := restful.CrossOriginResourceSharing{ExposeHeaders: []string{"Content-Type"}, CookiesAllowed: false, Container: container}
		ws.Filter(corsRule.Filter)
	}

	// Add webservice to container
	container.Add(ws)
}

func (d JobManager) WebService() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/She-Ra")
	ws.Consumes(restful.MIME_XML, restful.MIME_JSON).
	Produces(restful.MIME_JSON, restful.MIME_XML)

	ws.Route(ws.GET("/{job-id}").To(d.findJob).
		// docs
		Doc("get a job config").
		Operation("findJob").
		Param(ws.PathParameter("job-id", "identifier of the job").DataType("string")).
		Writes(Resource{})) // on the response

	ws.Route(ws.PUT("").To(d.createJob).
		// docs
		Doc("create a job").
		Operation("createJob").
		Reads(Job{})) // from the request

	ws.Route(ws.PUT("/{job-id}").To(d.updateJob).
		// docs
		Doc("update a job").
		Operation("updateJob").
		Param(ws.PathParameter("job-id", "identifier of the job").DataType("string")).
		Reads(Job{})) // from the request

	return ws
}
