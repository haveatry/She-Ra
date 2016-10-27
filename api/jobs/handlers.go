package jobs

import (
	"github.com/emicklei/go-restful"
	proto "github.com/golang/protobuf/proto"
//	"encoding/json"
    "She-Ra/configdata"
	//"She-Ra/util/linedmap"
	"She-Ra/util/lrumap"
    "log"
    "strconv"
	"net/http"
	"os"
	"io/ioutil"
	"runtime"
	"path"
	"errors"
	"sync"
)

const (
	WS_PATH = "/root/workspace"
	EXECUTION_PATH = "executions"
	MAX_EXEC_NUM = 100
	MAX_KEEP_DAYS = 3
)

type JobCommand struct {
	Name string
	Args []string
}



// local path in file system
var basePath string = "/home/jion_1/She-Ra"

type JobManager struct {
    JobMap *lrumap.LRU
    AccessLock *sync.RWMutex
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
	jober.Id = strconv.Itoa(d.JobMap.Len() + 1)
	// map key id:name after 2016/10
	key := jober.Name
	d.AccessLock.Lock()
	if d.JobMap.Contains(key) == true {
		d.AccessLock.Unlock()
		log.Print("job key already exist.")
		response.WriteErrorString(412, "job key already exist, please retry." )
		return	
	}
	
	if _, err := writeDataToFile(jober); err != nil {
		d.AccessLock.Unlock()
		log.Print("write job into file failed. key : ", key, "; ", jober)
		response.WriteErrorString(412, "write job into file failed, please retry." )
	}else{
		d.JobMap.Add(key, jober)	
		log.Print("add new job succeed; key: ", key, "; jober: ", jober)
	}
	d.AccessLock.Unlock()
	response.WriteHeaderAndEntity(http.StatusCreated, jober)
	
}

func writeDataToFile(job *configdata.Job) (fileName string, err error) {

	data, err := proto.Marshal(job)
	//data, err := json.Marshal(job)
	if err != nil {
        log.Fatal("marshaling error: ", err)
		return "", err	
	}
    log.Print("proto marshal: job", string(data))

	filePath := WS_PATH + "/" + job.Name
	if err = os.MkdirAll(filePath, 0777); err != nil {
		return  "", err
	}
	logPath  := filePath + "/log"
	if err = os.MkdirAll(logPath, 0777); err != nil {
		return "", err
	}
	fileName = filePath + "/" + job.Name
	//log.Print("job fileName: ", fileName)
	var file *os.File
	if isFileExist(fileName) != true {
		if file, err = os.Create(fileName); os.IsNotExist(err) {
			log.Print("file create failed : ", err)
			return "", err
		}else{
			log.Print("create file successfully.")
		}
		
	}else{
		if file, err = os.OpenFile(fileName, os.O_RDWR | os.O_TRUNC, 0666); os.IsNotExist(err) {
			log.Print(err)
			return fileName, err
		}else{
			log.Print("OpenFile ", fileName, " successfully.")
		}
	}
	var nLen int
	if nLen, err = file.Write(data); err != nil {
		log.Print("write data ino file ", fileName, " failed : ", string(data), ";data len: ", nLen)
		return fileName, err
	}else{
		log.Print("write data into file succeed. file: ", fileName, "; data: ", string(data))
	}
	if err = file.Close(); err != nil {
		log.Print("file close failed: ", err)
		return fileName, err
	}else{
		log.Print("file close succeed.")	
	}
	return fileName, err
}

func getLocalPath() string {
	var filePath string
	_, fullFileName, _, ok := runtime.Caller(0)
	if ok != false {
		filePath = path.Dir(fullFileName)
	}
	log.Print("get path :", filePath)
	return filePath
}

func isFileExist(fileName string) bool {
	var bExist bool
	if _, err := os.Stat(fileName); os.IsNotExist(err){
		log.Print("file is not exist, ", err)
		bExist = false 
	}else{
		bExist = true
		log.Print("file is exist.")
	}

	return bExist
}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {
	job_key := request.PathParameter("job-id")
	
	// first read data from database
	if ok := isExistJob(job_key); ok != false {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(410, "job-id: " + job_key +  " not find in database.")
	}else if  ok := d.JobMap.Contains(job_key); ok != true {
		// read data from file
		fName := WS_PATH + "/" + job_key + "/" + job_key
		if job, err := readJobFromFile(fName); err != nil {
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(410, "job-id: " + job_key +  " in database, not in memery; " + err.Error())
		}else{
			d.JobMap.Add(job_key, job)
			response.WriteEntity(job)
		}
	}else{
		if job, ok := d.JobMap.Get(job_key); ok != true {
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(410, "job-id: " + job_key +  "; get job from memery failed.")
		}else{
			response.WriteEntity(job)
			log.Print(job_key, " in memry.")
		}
	}
}

func readJobFromFile(fName string) (*configdata.Job, error) {
	if len(fName) == 0 {
		log.Print("fileName is empty.")
		return nil, errors.New("fileName is empty")
	}
	in, err := ioutil.ReadFile(fName)
	if err != nil {
        	log.Fatalln("Error reading file:", err)
		return nil, err
	}
	jObj := &configdata.Job{}
	if err := proto.Unmarshal(in, jObj); err != nil {
        	log.Fatalln("Failed to parse job:", err)
		return nil, err
	}
	return jObj, nil
	
} 

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {
	job_key := request.PathParameter("job-id")
	
	jober := new(configdata.Job)
	err := request.ReadEntity(jober)
	if err != nil {
		//response.AddHeader("Content-Type", "text/plain")
		//response.WriteErrorString(http.StatusInternalServerError, err.Error())
		errResponse(http.StatusInternalServerError, err.Error(), response)
		return
	}
	
	
	if ok := isExistJob(job_key); ok != true {
		errResponse(410, "job is not exist.", response)
	}else{
		if fileName, err := writeDataToFile(jober); err != nil {
			errResponse(412, err.Error() + "; fileName:" + fileName, response)
		}else{
			d.JobMap.Add(job_key, jober)	
			log.Print("add new job succeed; key: ", job_key, "; jober: ", jober)
			response.WriteHeaderAndEntity(http.StatusOK, jober)
		}
	}
}

func (d *JobManager) delJob(request *restful.Request, response *restful.Response) {
	job_key := request.PathParameter("job-id")
	if ok := isExistJob(job_key); ok != true {
		errResponse(410, "job is not exist.", response)
	}else{
		if ok = d.JobMap.Contains(job_key); ok == true {
			d.JobMap.Remove(job_key)
		}
		
		if err := os.RemoveAll(WS_PATH + "/" + job_key); err != nil {
			log.Print("when remove job , something occur: ", err)
		}
		// del from database
		
		response.WriteHeaderAndEntity(http.StatusOK, "remove job succeed.")
	}
}

func errResponse(status int, errInfo string,  response *restful.Response) {
	response.AddHeader("Content-Type", "text/plain")
	response.WriteErrorString(status, errInfo)
}

func isExistJob(job_id string) bool {
	// first read data from database
	return false
}

func (cmd *JobCommand) Exec(){

}
