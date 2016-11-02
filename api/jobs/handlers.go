package jobs

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/emicklei/go-restful"
	"github.com/haveatry/She-Ra/configdata"
	"github.com/haveatry/She-Ra/lru"
	. "github.com/haveatry/She-Ra/utils"
	_ "github.com/mattn/go-sqlite3"
	//"golang.org/x/net/websocket"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type JobManager struct {
	JobCache     *lru.ARCCache
	SeqNo        map[Key]int
	ExecChan     map[Key]chan int
	KillExecChan map[Key]chan int
	WaitExec     map[Key]*sync.WaitGroup
	db           *sql.DB
	//Content      string
	accessLock *sync.RWMutex
}

type JobCommand struct {
	Name string
	Args []string
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

func NewJobManager() (*JobManager, error) {
	if cache, err := lru.NewARC(100); err != nil {
		return nil, errors.New("init lru cache failed")
	} else {
		jobManager := &JobManager{
			JobCache:     cache,
			SeqNo:        make(map[Key]int, 100),
			ExecChan:     make(map[Key]chan int, 100),
			KillExecChan: make(map[Key]chan int, 100),
			WaitExec:     make(map[Key]*sync.WaitGroup, 100),
			db:           Database,
			accessLock:   &sync.RWMutex{},
		}
		return jobManager, nil
	}
}

func jobExists(key Key, cache *lru.ARCCache) bool {

	//check the job cache at first
	if cache.Contains(key) {
		return true
	}

	//take further check from the job config file
	if FileExists(WS_PATH + key.Ns + "/" + key.Id + "/.shera/configfile") {
		return true
	}

	return false
}

func (d *JobManager) createJob(request *restful.Request, response *restful.Response) {
	info("Enter createJob\n")
	ns := request.PathParameter("namespace")
	job := configdata.Job{}
	waitGroup := new(sync.WaitGroup)
	if err := request.ReadEntity(&job); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	job.MaxKeepDays = MAX_KEEP_DAYS
	job.MaxExecutionRecords = MAX_EXEC_NUM
	job.CurrentNumber = 0

	key := Key{Ns: ns, Id: job.Id}
	if jobExists(key, d.JobCache) {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, "job already exists")
		return
	}

	d.JobCache.Add(key, job)

	d.accessLock.Lock()
	d.SeqNo[key] = 0
	d.ExecChan[key] = make(chan int, 1)
	d.KillExecChan[key] = make(chan int, 1)
	d.WaitExec[key] = waitGroup
	d.accessLock.Unlock()

	createWorkSpace := &JobCommand{
		Name: "mkdir",
		Args: []string{"-p", WS_PATH + key.Ns + "/" + key.Id + "/.shera/" + EXECUTION_PATH},
	}
	createWorkSpace.Exec()

	//encode job info and store job info into config file
	if err := WriteData(key, &job); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	response.WriteHeaderAndEntity(http.StatusCreated, &job)
}

func (d *JobManager) findJob(request *restful.Request, response *restful.Response) {
	var job configdata.Job
	jobId := request.PathParameter("job-id")
	ns := request.PathParameter("namespace")
	key := Key{Ns: ns, Id: jobId}
	info("namespace is %s, job.Id: %s\n", ns, jobId)

	if !jobExists(key, d.JobCache) {
		info("failed to find the job %s\n", jobId)
		response.WriteHeader(http.StatusNotFound)
	} else if value, OK := d.JobCache.Get(key); OK {
		job = value.(configdata.Job)
		info("Get job successfully")
		response.WriteHeaderAndEntity(http.StatusFound, &job)
	} else if FileExists(WS_PATH + key.Ns + "/" + key.Id + "/.shera/configfile") {
		if err := ReadData(key, &job); err != nil {
			info("failed to read job config data")
			response.WriteHeader(http.StatusNotFound)
		} else {
			response.WriteHeaderAndEntity(http.StatusFound, &job)
		}
	} else {
		info("failed to find the job %s\n", jobId)
		response.WriteHeader(http.StatusNotFound)
	}
	return
}

func (d *JobManager) findAllJobs(request *restful.Request, response *restful.Response) {

}

func (d *JobManager) updateJob(request *restful.Request, response *restful.Response) {
	newJob := configdata.Job{}
	err := request.ReadEntity(&newJob)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	ns := request.PathParameter("namespace")
	key := Key{Ns: ns, Id: newJob.Id}
	if jobExists(key, d.JobCache) {
		d.JobCache.Add(key, newJob)

		//encode job info and store job info into config file
		if err = WriteData(key, &newJob); err != nil {
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(http.StatusInternalServerError, err.Error())
			return
		}

		response.WriteHeaderAndEntity(http.StatusAccepted, &newJob)
	} else {
		waitGroup := new(sync.WaitGroup)
		d.JobCache.Add(key, newJob)

		d.accessLock.Lock()
		d.SeqNo[key] = 0
		d.ExecChan[key] = make(chan int, 1)
		d.KillExecChan[key] = make(chan int, 1)
		d.WaitExec[key] = waitGroup
		d.accessLock.Unlock()

		createWorkSpace := &JobCommand{
			Name: "mkdir",
			Args: []string{"-p", WS_PATH + key.Ns + "/" + key.Id + "/.shera/" + EXECUTION_PATH},
		}
		createWorkSpace.Exec()

		//encode job info and store job info into config file
		if err = WriteData(key, &newJob); err != nil {
			response.AddHeader("Content-Type", "text/plain")
			response.WriteErrorString(http.StatusInternalServerError, err.Error())
			return
		}

		response.WriteHeaderAndEntity(http.StatusCreated, &newJob)

	}
	return
}

func (d *JobManager) delJob(request *restful.Request, response *restful.Response) {
	ns := request.PathParameter("namespace")
	jobId := request.PathParameter("job-id")
	key := Key{Ns: ns, Id: jobId}

	SetAllCancelled(key.Ns, key.Id)

	//Need to kill runnig execution of this job
	go func() {
		d.KillExecChan[key] <- EXEC_KILL_ALL
	}()

	//wait until all the running executions exit
	d.WaitExec[key].Wait()

	DeleteJobExecutions(key.Ns, key.Id)

	cleanupCmd := &JobCommand{
		Name: "rm",
		Args: []string{"-rf", WS_PATH + ns + "/" + jobId},
	}

	success := cleanupCmd.Exec()
	if !success {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	go func() {
		//read KillExecChan again to ensure it unblocked
		<-d.KillExecChan[key]
	}()

	d.JobCache.Remove(key)

	d.accessLock.Lock()
	info("delJob:get access lock successfully")
	close(d.KillExecChan[key])
	close(d.ExecChan[key])
	delete(d.SeqNo, key)
	delete(d.ExecChan, key)
	delete(d.KillExecChan, key)
	delete(d.WaitExec, key)
	d.accessLock.Unlock()

	response.WriteHeader(http.StatusAccepted)
}

func (d *JobManager) execJob(request *restful.Request, response *restful.Response) {
	ns := request.PathParameter("namespace")
	jobId := request.PathParameter("job-id")
	key := Key{Ns: ns, Id: jobId}

	info("execJob: key.ns=%s, key.id=%s\n", key.Ns, key.Id)
	if d.JobCache.Contains(key) {
		d.accessLock.Lock()
		jobExec := &configdata.Execution{}
		jobExec.Number = int32(d.SeqNo[key] + 1)
		now := time.Now()
		year, mon, day := now.Date()
		hour, min, sec := now.Clock()
		jobExec.LogFile = fmt.Sprintf("%03d-%d%02d%02d%02d%02d%02d", int(jobExec.Number), year, mon, day, hour, min, sec)
		jobExec.Progress = configdata.Execution_INIT
		jobExec.EndStatus = configdata.Execution_FAILURE
		response.WriteHeaderAndEntity(http.StatusCreated, jobExec)

		d.SeqNo[key] = d.SeqNo[key] + 1
		InsertExecutionRecord(key.Ns, key.Id, int(jobExec.Number), 0, 0, 0)
		info("Get the write lock successfully")
		d.WaitExec[key].Add(1)
		go d.runJobExecution(key, int(jobExec.Number))
		d.accessLock.Unlock()
		return
	} else {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, "no such job found")
		return
	}
}

func (d *JobManager) runJobExecution(key Key, seqno int) {
	var retCode int
	d.ExecChan[key] <- EXEC_GOROUTINE
	if value, OK := d.JobCache.Get(key); OK {
		info("key.Ns=%s, key.Id=%s, seqno=%d\n", key.Ns, key.Id, seqno)
		if cancelStat := GetCancelStatus(key.Ns, key.Id, seqno); cancelStat == 1 {
			d.accessLock.Lock()
			<-d.ExecChan[key]
			d.WaitExec[key].Done()
			d.accessLock.Unlock()
			return
		}

		job := value.(configdata.Job)
		progress := configdata.Execution_INIT
		info("begin to execute command")

		//change the working dir
		targetPath, err := filepath.Abs(WS_PATH + key.Ns + "/" + key.Id)
		if err != nil {
			d.accessLock.Lock()
			<-d.ExecChan[key]
			d.WaitExec[key].Done()
			d.accessLock.Unlock()
			log.Fatalf("AbsError (%s): %s\\n", WS_PATH+key.Ns+"/"+key.Id, err)

			return
		}

		info("Target Path: %s\\n", targetPath)
		err = os.Chdir(targetPath)
		if err != nil {
			d.accessLock.Lock()
			<-d.ExecChan[key]
			d.WaitExec[key].Done()
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

		if retCode = switchJdkCmd.ExecAsync(d, key, startTime, seqno, configdata.Execution_INIT); retCode != EXEC_FINISHED {
			d.accessLock.Lock()
			<-d.ExecChan[key]
			d.WaitExec[key].Done()
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
			if retCode = gitInitCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[key]
				d.WaitExec[key].Done()
				d.accessLock.Unlock()
				return
			}

			gitConfigCmd := &JobCommand{
				Name: "git",
				Args: []string{"config", "remote.origin.url", codeManager.GitConfig.Repo.Url},
			}
			if retCode = gitConfigCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[key]
				d.WaitExec[key].Done()
				d.accessLock.Unlock()
				return
			}

			gitPullCmd := &JobCommand{
				Name: "git",
				Args: []string{"pull", "origin", codeManager.GitConfig.Branch},
			}
			if retCode = gitPullCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[key]
				d.WaitExec[key].Done()
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
				if retCode = antBuildCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
					d.accessLock.Lock()
					<-d.ExecChan[key]
					d.WaitExec[key].Done()
					d.accessLock.Unlock()
					return
				}
			}

			if buildManager.MvnConfig != nil {
				mvnBuildCmd := &JobCommand{
					Name: "mvn",
					Args: []string{"-f", buildManager.MvnConfig.Pom, buildManager.MvnConfig.Goals},
				}

				if retCode = mvnBuildCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
					d.accessLock.Lock()
					<-d.ExecChan[key]
					d.WaitExec[key].Done()
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
			if retCode = imgBuildCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[key]
				d.WaitExec[key].Done()
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
			if retCode = imgPushCmd.ExecAsync(d, key, startTime, seqno, progress); retCode != EXEC_FINISHED {
				d.accessLock.Lock()
				<-d.ExecChan[key]
				d.WaitExec[key].Done()
				d.accessLock.Unlock()
				return
			}
		}
		duration := time.Now().Sub(startTime).Seconds()
		UpdateExecutionRecord(key.Ns, key.Id, seqno, int(duration), int(progress), int(configdata.Execution_SUCCESS), 1)
		d.accessLock.Lock()
		<-d.ExecChan[key]
		d.WaitExec[key].Done()
		d.accessLock.Unlock()
	}

}

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
	ns := request.PathParameter("namespace")
	jobId := request.PathParameter("job-id")
	if seqno, err := strconv.Atoi(request.PathParameter("execution_id")); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	} else {
		key := Key{Ns: ns, Id: jobId}
		//update the execution as cancelled in the database
		SetExecutionCancelled(key.Ns, key.Id, seqno)
		go func() {
			d.KillExecChan[key] <- seqno
		}()

		response.WriteHeader(http.StatusAccepted)
		return
	}
}

func (cmd *JobCommand) ExecAsync(d *JobManager, key Key, startTime time.Time, number int, progress configdata.Execution_State) int {

	var recvCode int
	jobCmd := exec.Command(cmd.Name, cmd.Args...)
	jobCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := jobCmd.Start()
	if err != nil {
		info("err occurred when start executing command: (cmd=%s, agrs=%v): \\n%v\\n", cmd.Name, cmd.Args)
		duration := time.Now().Sub(startTime).Seconds()
		UpdateExecutionRecord(key.Ns, key.Id, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
		return EXEC_ERROR
	}

	done := make(chan error)
	go func() {
		done <- jobCmd.Wait()
	}()

	for {
		select {
		case recvCode = <-d.KillExecChan[key]:
			info("received kill execution command %d, seqno:%d\n", recvCode, number)
			if recvCode == number || recvCode == EXEC_KILL_ALL {
				info("begin to kill the execution command\n")
				pgid, err := syscall.Getpgid(jobCmd.Process.Pid)
				if err == nil {
					syscall.Kill(-pgid, syscall.SIGTERM)
				}
				info("kill the execution command successfully\n")
				duration := time.Now().Sub(startTime).Seconds()
				UpdateExecutionRecord(key.Ns, key.Id, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
				return recvCode
			}
			break

		case err = <-done:
			if err != nil {
				info("process done with error = %v\n", err)
				duration := time.Now().Sub(startTime).Seconds()
				UpdateExecutionRecord(key.Ns, key.Id, int(number), int(duration), int(progress), int(configdata.Execution_FAILURE), 1)
				return EXEC_ERROR
			} else {
				return EXEC_FINISHED
			}

		}
	}
}

// Log wrapper
func info(template string, values ...interface{}) {
	log.Printf("[She-Ra][info] "+template+"\n", values...)
}
