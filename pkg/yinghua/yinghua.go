package yinghua

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
	"strconv"
	"time"
	"yinghua/pkg/config"
	"yinghua/pkg/util"
	"yinghua/pkg/yinghua/types"
)

type YingHua struct {
	User    config.User
	Courses []types.CoursesList
	client  *resty.Client
}

func New(user config.User) *YingHua {

	var client = resty.New()
	client.SetBaseURL(user.BaseURL)
	client.SetRetryCount(3)
	return &YingHua{
		User:   user,
		client: client,
	}

}

func (i *YingHua) Login() error {

	resp := new(types.LoginResponse)
	resp2, err := i.client.R().SetFormData(map[string]string{
		"platform":  "Android",
		"username":  i.User.Username,
		"password":  i.User.Password,
		"pushId":    "140fe1da9e67b9c14a7",
		"school_id": strconv.Itoa(i.User.SchoolID),
		"imgSign":   "533560501d19cc30271a850810b09e3e",
		"imgCode":   "cryd",
	}).
		SetResult(resp).
		Post("/api/login.json")

	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return errors.New(resp.Msg)
	}

	i.client.SetCookies(resp2.Cookies())

	i.User.Token = resp.Result.Data.Token

	return nil

}

func (i *YingHua) GetCourses() error {

	resp := new(types.CoursesResponse)
	_, err := i.client.R().
		SetResult(resp).
		SetFormData(map[string]string{
			"token": i.User.Token,
		}).
		Post("/api/course.json")

	if err != nil {
		return err
	}

	if resp.Code != 0 {
		return errors.New(resp.Msg)
	}
	i.Courses = resp.Result.List
	return nil
}

func (i *YingHua) GetChapters(course types.CoursesList) ([]types.ChaptersList, error) {

	resp := new(types.ChaptersResponse)
	_, err := i.client.R().
		SetResult(resp).
		SetFormData(map[string]string{
			"token":    i.User.Token,
			"courseId": strconv.Itoa(course.ID),
		}).
		Post("/api/course/chapter.json")

	if err != nil {
		return nil, err
	}

	if resp.Code != 0 {
		return nil, errors.New(resp.Msg)
	}
	return resp.Result.List, nil
}

func (i *YingHua) StudyCourse(course types.CoursesList) error {
	chapters, err := i.GetChapters(course)
	if err != nil {
		return err
	}
	for _, chapter := range chapters {
		i.StudyChapter(chapter)
	}

	return nil
}

func (i YingHua) StudyChapter(chapter types.ChaptersList) {

	i.Output(fmt.Sprintf("当前第 %d 章, [%s][chapterId=%d]", chapter.Idx, chapter.Name, chapter.ID))
	for _, node := range chapter.NodeList {
		// 试题跳过
		if node.TabVideo {
			i.StudyNode(node)
		}
	}

}

func (i YingHua) StudyNode(node types.ChaptersNodeList) {

	i.Output(fmt.Sprintf("当前第 %d 课, [%s][nodeId=%d]", node.Idx, node.Name, node.ID))
	studyTime := 1
	studyId := 0
	nodeProgress := types.NodeVideoData{
		StudyTotal: types.NodeVideoStudyTotal{
			Progress: "0.00",
		},
	}
	go func() {
		for true {
			nodeProgress = i.GetNodeProgress(node)
			if nodeProgress.StudyTotal.State == "2" {
				node.VideoState = 2
				break
			}
			time.Sleep(time.Second * 10)
		}
	}()

	for node.VideoState != 2 {
	captcha:
		var resp = new(types.StudyNodeResponse)
		formData := map[string]string{
			"nodeId":    strconv.Itoa(node.ID),
			"token":     i.User.Token,
			"studyTime": strconv.Itoa(studyTime),
			"studyId":   strconv.Itoa(studyId),
		}
		_, err := i.client.R().
			SetFormData(formData).
			SetResult(resp).
			Post("/api/node/study.json")
		if err != nil {
			i.OutputWith(err.Error(), logrus.Errorf)
			continue
		}
		if resp.Code != 0 {
			i.OutputWith(resp.Msg, logrus.Errorf)
			if resp.NeedCode {
				formData["code"] = i.FuckCaptcha() + "_"
				goto captcha
			}
			break
		}
		studyId = resp.Result.Data.StudyID
		if nodeProgress.StudyTotal.Progress == "" {
			nodeProgress.StudyTotal.Progress = "0"

		}
		parseFloat, err := strconv.ParseFloat(nodeProgress.StudyTotal.Progress, 64)

		if err != nil {
			i.OutputWith(err.Error(), logrus.Errorf)
			continue
		}
		i.Output(fmt.Sprintf("%s[nodeId=%d], %s[studyId=%d], 当前进度: %.f%%", node.Name, node.ID, resp.Msg, studyId, parseFloat*100))
		studyTime += 10
		time.Sleep(time.Second * 10)
	}
}

func (i YingHua) GetNodeProgress(node types.ChaptersNodeList) types.NodeVideoData {

	var resp = new(types.NodeVideoResponse)
	_, err := i.client.R().
		SetFormData(map[string]string{
			"nodeId": strconv.Itoa(node.ID),
			"token":  i.User.Token,
		}).
		SetResult(resp).
		Post("/api/node/video.json")
	if err != nil {
		i.OutputWith(err.Error(), logrus.Errorf)
	}
	if resp.Code != 0 {
		i.OutputWith(resp.Msg, logrus.Errorf)
	}
	return resp.Result.Data
}

func (i YingHua) FuckCaptcha() string {

	i.Output("正在识别验证码")
	response, err := i.client.R().
		Get(fmt.Sprintf("/service/code/aa?t=%d", time.Now().UnixNano()))

	if err != nil {
		i.OutputWith(err.Error(), logrus.Errorf)
	}
	var resp = new(types.Captcha)
	client := resty.New()
	_, err = client.R().
		SetFileReader("file", "image.png", bytes.NewReader(response.Body())).
		SetResult(resp).
		Post("https://api.opop.vip/captcha/recognize")

	if err != nil {
		i.OutputWith(err.Error(), logrus.Errorf)
	}
	if resp.Status != "ok" {
		i.OutputWith(resp.Message, logrus.Errorf)
	}
	s := resp.Data.(string)
	i.Output(fmt.Sprintf("验证码识别成功: %s", s))
	return s
}
func (i YingHua) Output(message string) {
	i.OutputWith(message, logrus.Infof)
}

func (i YingHua) OutputWith(message string, writer func(format string, args ...interface{})) {
	writer("[协程ID=%d][%s] %s", util.GetGid(), i.User.Username, message)
}
