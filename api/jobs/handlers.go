package jobs

import (
	"database/sql"
	"fmt"
	"github.com/emicklei/go-restful"
	"github.com/golang/protobuf/proto"
	"github.com/haveatry/She-Ra/configdata"
	_ "github.com/mattn/go-sqlite3"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	WS_PATH           = "/workspace/"
	EXECUTION_PATH    = "executions/"
	MAX_EXEC_NUM      = 100
	MAX_KEEP_DAYS     = 3
	KILL_EXECUTION    = 886
	EXEC_GOROUTINE    = 1314
	EXEC_NOT_START    = 0
	EXEC_FINISHED     = 1
	EXEC_KILL_ONE     = 2
	EXEC_KILL_ALL     = 4
	EXEC_KILL_FAILURE = 8
	EXEC_ERROR        = 16
	EXEC_INIT         = 32
)

type JobManager struct {
	JobMap       map[string]*configdata.Job
	ExecChan     map[string]chan int
	KillExecChan map[string]chan int
	WaitExec     map[string]*sync.WaitGroup
	db           *sql.DB
	accessLock   *sync.RWMutex
}

/*
type JobExecInternal struct {
	CommandList []string
	Finish      chan int
}
*/

/*
type GitClient struct {
}
*/

type JobCommand struct {
	Name string
	Args []string
}

func NewJobManager() (*JobManager, error) {
	var err error
	jobManager := &JobManager{
		JobMap:       make(map[string]*configdata.Job, 100),
		ExecChan:     make(map[string]chan int, 100),
		KillExecChan: make(map[string]chan int, 100),
		WaitExec:     make(map[string]*sync.WaitGroup, 100),
		accessLock:   &sync.RWMutex{},
	}

	//create database for She-Ra project
	if jobManager.db, err = sql.Open("sqlite3", "/workspace/She-Ra.db"); err != nil {
		info("failed to setup database")
	}

	//`create table job to store execution information
	sql := `Create table job(jobId string(100),number int,duration int,progress int,status int, finished int)`
	if _, err = jobManager.db.Exec(sql); err != nil {
		info("failed to create table job")
	}

	return jobManager, err
}

func (d *JobManager) insertExecutionRecord(jobId string, number, duration, progress, status int) {
	if stmt, err := d.db.Prepare("insert into job(jobId, number, duration, progress, status,finished) values (?,?,?,?,?,?)"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare insert sql:%v\n", err)
	} else if _, err := stmt.Exec(jobId, number, duration, progress, status, 0); err != nil {
		log.Fatalf("[She-Ra][error] failed to insert data into database:%v\n", err)
	}
}

func (d *JobManager) updateExecutionRecord(jobId string, number, duration, progress, status, finished int) {
	if stmt, err := d.db.Prepare("update job set duration=?, progress=?, status=?, finished=? where jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare update sql:%v\n", err)
	} else if _, err := stmt.Exec(duration, progress, status, finished, jobId, number); err != nil {
		log.Fatalf("[She-Ra][error] failed to update data in database:%v\n", err)
	}
}

func (d *JobManager) deleteExecutionRecord(jobId string, number int) {
	if stmt, err := d.db.Prepare("delete from job where jobId=? and number=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare delete sql:%v\n", err)
	} else if _, err := stmt.Exec(jobId, number); err != nil {
		log.Fatalf("[She-Ra][error] failed to delete job execution in database:%v\n", err)
	}
}

func (d *JobManager) deleteJobExecutions(jobId string) {
	if stmt, err := d.db.Prepare("delete from job where jobId=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare delete sql:%v\n", err)
	} else if _, err := stmt.Exec(jobId); err != nil {
		log.Fatalf("[She-Ra][error] failed to delete job executions in database:%v\n", err)
	}
}

func (d *JobManager) getRunningCount(jobId string, finished int) int {
	var count int
	if stmt, err := d.db.Prepare("select count(*) from job where jobId =? and finished=?"); err != nil {
		log.Fatalf("[She-Ra][error] failed to prepare query sql:%v\n", err)
	} else if err := stmt.QueryRow(jobId, finished).Scan(&count); err != nil {
		log.Fatalf("[She-Ra][error] failed to get the tatoal running executions:%v\n", err)
	}
	return count
}

func (d *JobManager) createJob(request *restful.Request, response *restful.Response) {
	job := new(configdata.Job)
	waitGroup := new(sync.WaitGroup)
	err := request.ReadEntity(job)
	info("job.Id: %s\n", job.Id)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0

	d.accessLock.RLock()
	_, OK := d.JobMap[job.Id]
	d.accessLock.RUnlock()
	if OK {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	} else {
		d.accessLock.Lock()
		d.ExecChan[job.Id] = make(chan int, 1)
		d.KillExecChan[job.Id] = make(chan int, 1)
		d.JobMap[job.Id] = job
		d.WaitExec[job.Id] = waitGroup
		d.accessLock.Unlock()
	}

	//encode job info and store job info into config file
	out, err := proto.Marshal(job)
	if err != nil {
		log.Fatalln("Failed to encode job info:", err)
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	createWorkSpace := &JobCommand{
		Name: "mkdir",
		Args: []string{"-p", WS_PATH + job.Id + "/.shera/" + EXECUTION_PATH},
	}
	createWorkSpace.Exec()

	configFile := WS_PATH + job.Id + "/.shera/configfile"
	if err := ioutil.WriteFile(configFile, out, 0644); err != nil {
		log.Fatalln("Failed to write job info to file:", err)
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	response.WriteHeaderAndEntity(http.StatusCreated, job)
}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("job.Id: %s\n", jobId)

	d.accessLock.RLock()
	info("Get the read lock successfully")
	job, OK := d.JobMap[jobId]
	d.accessLock.RUnlock()
	if OK {
		info("Get job successfully")
		response.WriteHeaderAndEntity(http.StatusFound, job)
	} else {
		info("failed to find the job %s\n", jobId)
		response.WriteHeader(http.StatusNotFound)
	}
}

func (d *JobManager) findAllJobs(request *restful.Request, response *restful.Response) {

}

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {
	job := new(configdata.Job)
	err := request.ReadEntity(job)
	info("job.Id: %s\n", job.Id)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0
	d.accessLock.Lock()
	d.JobMap[job.Id] = job
	d.accessLock.Unlock()

	response.WriteHeaderAndEntity(http.StatusCreated, job)
}

func (d *JobManager) delJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("jober.Id: %s", jobId)

	d.accessLock.Lock()
	d.JobMap[jobId].JobRemoved = 1
	d.accessLock.Unlock()

	//Need to kill runnig execution of this job
	go func() {
		d.KillExecChan[jobId] <- EXEC_KILL_ALL
	}()

	//wait until all the running executions exit
	d.WaitExec[jobId].Wait()

	d.deleteJobExecutions(jobId)

	cleanupCmd := &JobCommand{
		Name: "rm",
		Args: []string{"-rf", WS_PATH + jobId},
	}
	success := cleanupCmd.Exec()
	if !success {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	go func() {
		//read KillExecChan again to ensure it unblocked
		<-d.KillExecChan[jobId]
		close(d.KillExecChan[jobId])
	}()

	d.accessLock.Lock()
	info("delJob:get access lock successfully")
	close(d.ExecChan[jobId])
	delete(d.JobMap, jobId)
	delete(d.ExecChan, jobId)
	delete(d.KillExecChan, jobId)
	delete(d.WaitExec, jobId)
	d.accessLock.Unlock()

	response.WriteHeader(http.StatusAccepted)
}

func (d *JobManager) execJob(request *restful.Request, response *restful.Response) {
	jobId := request.PathParameter("job-id")
	info("job.Id: %s\n", jobId)

	d.accessLock.RLock()
	info("Get the read lock successfully")
	job, OK := d.JobMap[jobId]
	d.accessLock.RUnlock()
	if OK {
		info("Get job successfully")
		jobExec := &configdata.Execution{}
		jobExec.Number = job.CurrentNumber + 1
		now := time.Now()
		year, mon, day := now.Date()
		hour, min, sec := now.Clock()
		jobExec.LogFile = fmt.Sprintf("%03d-%d%02d%02d%02d%02d%02d", int(jobExec.Number), year, mon, day, hour, min, sec)
		jobExec.Progress = configdata.Execution_INIT
		jobExec.EndStatus = configdata.Execution_SUCCESS
		response.WriteHeaderAndEntity(http.StatusCreated, jobExec)

		d.accessLock.Lock()

		d.JobMap[jobId].CurrentNumber++
		d.insertExecutionRecord(jobId, int(jobExec.Number), 0, 0, 0)
		info("Get the write lock successfully")
		d.WaitExec[jobId].Add(1)
		go d.runJobExecution(jobId, int(d.JobMap[jobId].CurrentNumber))
		d.accessLock.Unlock()

	} else {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, "no such job found")
		return
	}
	/*
		configFile := WS_PATH + jobId
		in, err := ioutil.ReadFile(configFile)
		if err != nil {
			if os.IsNotExist(err) {
				log.Println("%s: File not found.  Creating new file.\n", configFile)

			} else {
				log.Fatalln("Error reading file:", err)
			}
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(http.StatusInternalServerError, err.Error())
		}

		job := &configdata.Job{}
		if err := proto.Unmarshal(in, job); err != nil {
			log.Fatalln("Failed to parse job info:", err)
		}
	*/
}

func (cmd *JobCommand) Exec() bool {
	var (
		cmdOut []byte
		err    error
	)

	if cmdOut, err = exec.Command(cmd.Name, cmd.Args...).Output(); err != nil {
		info("Failed to execute command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return false
	}

	info("Output (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args, string(cmdOut))
	return true
}

func (cmd *JobCommand) ExecAsync(d *JobManager, jobId string, startTime time.Time, number int, progress configdata.Execution_State) int {

	var recvCode int
	jobCmd := exec.Command(cmd.Name, cmd.Args...)
	err := jobCmd.Start()
	if err != nil {
		info("err occurred when start executing command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		duration := time.Now().Sub(startTime).Seconds()
		d.updateExecutionRecord(jobId, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
		return EXEC_ERROR
	}

	done := make(chan error)
	go func() {
		done <- jobCmd.Wait()
	}()

	select {
	case recvCode = <-d.KillExecChan[jobId]:
		if err = jobCmd.Process.Kill(); err != nil {
			log.Fatal("failed to kill: ", err)
			return EXEC_ERROR
		}

		info("received kill execution command\n")
		if recvCode == EXEC_KILL_ONE {
			duration := time.Now().Sub(startTime).Seconds()
			d.updateExecutionRecord(jobId, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
		} else if recvCode == EXEC_KILL_ALL {
			duration := time.Now().Sub(startTime).Seconds()
			d.updateExecutionRecord(jobId, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
		}
		return recvCode

	case err = <-done:
		if err != nil {
			info("process done with error = %v\n", err)
			duration := time.Now().Sub(startTime).Seconds()
			d.updateExecutionRecord(jobId, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
			return EXEC_ERROR
		}

	}
	return EXEC_FINISHED
}

func (cmd *JobCommand) ExecPipeCmd(in *JobCommand) int {
	var err error

	producer := exec.Command(in.Name, in.Args...)
	consumer := exec.Command(cmd.Name, cmd.Args...)
	if consumer.Stdin, err = producer.StdoutPipe(); err != nil {
		info("Failed to combine the 2 commands with pipe\n")
		return EXEC_ERROR
	}

	if err = consumer.Start(); err != nil {
		info("err occurred when start executing command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return EXEC_ERROR
	}

	if err = producer.Run(); err != nil {
		info("err occurred when executing command: (cmd=%s, agrs=%v): \\n%v\\n", in.Name, in.Args)
		return EXEC_ERROR
	}

	if err = consumer.Wait(); err != nil {
		info("err occurred when waiting the command executing complete: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		return EXEC_ERROR
	}

	return EXEC_FINISHED

}

func (d *JobManager) runJobExecution(jobId string, seqno int) {
	var retCode int
	d.ExecChan[jobId] <- EXEC_GOROUTINE
	d.accessLock.RLock()
	job, OK := d.JobMap[jobId]
	d.accessLock.RUnlock()
	if OK {
		if job.JobRemoved == 1 {
			d.accessLock.Lock()
			<-d.ExecChan[jobId]
			d.WaitExec[jobId].Done()
			d.accessLock.Unlock()
			return

		}
		var progress configdata.Execution_State
		progress = configdata.Execution_INIT
		info("begin to execute command")

		//change the working dir
		targetPath, err := filepath.Abs(WS_PATH + jobId)
		if err != nil {
			d.accessLock.Lock()
			<-d.ExecChan[jobId]
			d.WaitExec[jobId].Done()
			d.accessLock.Unlock()
			info("AbsError (%s): %s\\n", WS_PATH+jobId, err)

			return
		}

		info("Target Path: %s\\n", targetPath)
		err = os.Chdir(targetPath)
		if err != nil {
			d.accessLock.Lock()
			<-d.ExecChan[jobId]
			d.WaitExec[jobId].Done()
			d.accessLock.Unlock()
			info("ChdirError (%s): %s\\n", targetPath, err)
			return
		}

		startTime := time.Now()
		//select correct jdk version
		switchJdkCmd := &JobCommand{
			Name: "bash",
			Args: []string{"-c", "echo 1 | alternatives --config java"},
		}

		if job.JdkVersion == "jdk1.7" {
			switchJdkCmd.Args = []string{"-c", "echo 2 | alternatives --config java"}
		}

		if retCode = switchJdkCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_INIT); retCode != EXEC_FINISHED {
			d.accessLock.Lock()
			<-d.ExecChan[jobId]
			d.WaitExec[jobId].Done()
			d.accessLock.Unlock()
			return
		}

		//pull code from git
		if codeManager := job.GetCodeManager(); codeManager != nil && codeManager.GitConfig != nil {
			info("begin to pulling code\n")
			progress = configdata.Execution_CODE_PULLING
			gitInitCmd := &JobCommand{
				Name: "git",
				Args: []string{"init"},
			}
			if retCode = gitInitCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_CODE_PULLING); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[jobId]
				d.WaitExec[jobId].Done()
				d.accessLock.Unlock()
				return
			}

			gitConfigCmd := &JobCommand{
				Name: "git",
				Args: []string{"config", "remote.origin.url", codeManager.GitConfig.Repo.Url},
			}
			if retCode = gitConfigCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_CODE_PULLING); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[jobId]
				d.WaitExec[jobId].Done()
				d.accessLock.Unlock()
				return
			}

			gitPullCmd := &JobCommand{
				Name: "git",
				Args: []string{"pull", "origin", codeManager.GitConfig.Branch},
			}
			if retCode = gitPullCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_CODE_PULLING); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[jobId]
				d.WaitExec[jobId].Done()
				d.accessLock.Unlock()
				return
			}
		}

		if buildManager := job.GetBuildManager(); buildManager != nil {
			progress = configdata.Execution_CODE_BUILDING
			if buildManager.AntConfig != nil {
				antBuildCmd := &JobCommand{
					Name: "ant",
					Args: []string{"-f", buildManager.AntConfig.BuildFile, "-D" + buildManager.AntConfig.Properties},
				}
				if retCode = antBuildCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_CODE_BUILDING); retCode != EXEC_FINISHED {
					d.accessLock.Lock()
					<-d.ExecChan[jobId]
					d.WaitExec[jobId].Done()
					d.accessLock.Unlock()
					return
				}
			}

			if buildManager.MvnConfig != nil {
				mvnBuildCmd := &JobCommand{
					Name: "mvn",
					Args: []string{"-f", buildManager.MvnConfig.Pom, buildManager.MvnConfig.Goals},
				}

				if retCode = mvnBuildCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_CODE_BUILDING); retCode != EXEC_FINISHED {
					d.accessLock.Lock()
					<-d.ExecChan[jobId]
					d.WaitExec[jobId].Done()
					d.accessLock.Unlock()
					return
				}
			}
		}

		if job.BuildImgCmd != "" {
			progress = configdata.Execution_IMAGE_BUILDING
			cmdWithArgs := strings.Split(job.BuildImgCmd, " ")
			imgBuildCmd := &JobCommand{
				Name: cmdWithArgs[0],
				Args: cmdWithArgs[1:],
			}
			if retCode = imgBuildCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_IMAGE_BUILDING); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[jobId]
				d.WaitExec[jobId].Done()
				d.accessLock.Unlock()
				return
			}
		}

		if job.PushImgCmd != "" {
			progress = configdata.Execution_IMAGE_PUSHING
			cmdWithArgs := strings.Split(job.PushImgCmd, " ")
			imgPushCmd := &JobCommand{
				Name: cmdWithArgs[0],
				Args: cmdWithArgs[1:],
			}
			if retCode = imgPushCmd.ExecAsync(d, jobId, startTime, seqno, configdata.Execution_IMAGE_PUSHING); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[jobId]
				d.WaitExec[jobId].Done()
				d.accessLock.Unlock()
				return
			}
		}
		duration := time.Now().Sub(startTime).Seconds()
		d.updateExecutionRecord(jobId, seqno, int(duration), int(progress), int(configdata.Execution_SUCCESS), 1)
		d.accessLock.Lock()
		<-d.ExecChan[jobId]
		d.WaitExec[jobId].Done()
		d.accessLock.Unlock()
	}
}

/*
func (d *JobManager) monitorAllJobExecutions() {
	for {
		time.Sleep(5 * time.Second)
		for jobId, jobChan := range d.ExecChan {
			select {
			case cmd, OK := <-jobChan:
				if !OK {
					info("channel has been closed\n")
					break
				}

				if cmd == 0 {
					//this job has been deleted, exit this goroutine
					info("command value is %d\n", cmd)
					break
				}

				if cmd == EXEC_GOROUTINE {
					go d.runJobExecution(jobId)
				}
			default:
				info("Enter default %s", jobId)

			}

		}
	}
}
*/

//watch one job execution status change
func (d *JobManager) watchJobExecution(request *restful.Request, response *restful.Response) {

}

//open on job execution record
func (d *JobManager) openJobExecution(request *restful.Request, response *restful.Response) {

}

//get the job execution list
func (d *JobManager) getAllJobExecutions(request *restful.Request, response *restful.Response) {

}

//delete one job execution record
func (d *JobManager) delJobExecution(request *restful.Request, response *restful.Response) {

}

//force stop one job execution
func (d *JobManager) killJobExecution(request *restful.Request, response *restful.Response) {

}

// Log wrapper
func info(template string, values ...interface{}) {
	log.Printf("[She-Ra][info] "+template+"\n", values...)
}
