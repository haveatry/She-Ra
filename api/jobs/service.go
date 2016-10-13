package jobs

import (
	"encoding/json"
	"errors"
	"github.com/emicklei/go-restful"
	"github.com/golang/protobuf/proto"
	. "github.com/haveatry/She-Ra/api/response"
	. "github.com/haveatry/She-Ra/configdata"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func WebService(jobMng *JobManager) *restful.WebService {

}

func Register(jobMng *JobManager, container *restful.Container, cors bool) {
	dc := Resource{jobMng}
	dc.Register(container, cors)
}

func (d Resource) Register(container *restful.Container, cors bool) {
	ws := d.WebService()

	// Cross Origin Resource Sharing filter
	if cors {
		corsRule := restful.CrossOriginResourceSharing{ExposeHeaders: []string{"Content-Type"}, CookiesAllowed: false, Container: container}
		ws.Filter(corsRule.Filter)
	}

	// Add webservice to container
	container.Add(ws)
}

func (d Resource) WebService() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/She-Ra")
	ws.Consumes("*/*")
	ws.Produces(restful.MIME_JSON)

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
		Reads(Resource{})) // from the request

	ws.Route(ws.PUT("/{job-id}").To(d.updateJob).
		// docs
		Doc("update a job").
		Operation("updateJob").
		Param(ws.PathParameter("job-id", "identifier of the job").DataType("string")).
		Reads(Resource{})) // from the request

	return ws
}
