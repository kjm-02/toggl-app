package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// Work読み取り用
type WorkReader interface {
	GetWorks(auth0_id, date string) ([]Works, int, error)
	GetSummary(auth0_id, date string) ([]WorkSummary, error)
}

type RealRepo struct {
	DB *sql.DB
}

// Work書き込み用
type WorkWriter interface {
	CreateWork(auth0_id string, req reportRequestBody) error
	EndWork(auth0_id string, req reportRequestBody) error
	UpdateWork(auth0_id string, req Works, work_id string) error
	DeleteWork(auth0_id string, work_id string) error
}

func (r RealRepo) GetWorks(auth0_id, report_date string) ([]Works, int, error) {
	// report_idからすべてのworkを検索する
	rows, err_work := r.DB.Query(`
		SELECT work_entry_id, project_name, work_class, task_name, start_time, end_time 
		FROM workentries w 
		JOIN dailyreports d ON d.report_id = w.report_id 
		WHERE d.auth0_id = ? AND d.report_date = ?
		ORDER BY start_time`, auth0_id, report_date)
	if err_work != nil {
		return nil, -60, fmt.Errorf("get works: %w", err_work)
	}

	defer rows.Close()

	var works []Works

	for rows.Next() {
		var work_id string
		var project_name string
		var work_class string
		var task_name string
		var start_time string
		var end_time sql.NullString // end_timeはnilが入ることがある
		if err := rows.Scan(&work_id, &project_name, &work_class, &task_name, &start_time, &end_time); err != nil {
			return nil, -60, fmt.Errorf("get works: %w", err)
		}

		if len(start_time) >= 5 {
			start_time = start_time[:5] // 秒を消す
		}
		if end_time.Valid {
			if len(end_time.String) >= 5 {
				end_time.String = end_time.String[:5]
			}
		}

		works = append(works, Works{
			Work_id:      work_id,
			Project_name: project_name,
			Work_class:   work_class,
			Task_name:    task_name,
			Start_time:   start_time,
			End_time:     end_time.String,
			Memo:         "",
		})
	}

	sum := -60

	for i := range works {
		start, _ := stringToTime("2006-01-01 " + works[i].Start_time)
		end, end_err := stringToTime("2006-01-01 " + works[i].End_time)

		diff := 0
		if end_err == nil {
			diff = int(end.Sub(start).Minutes())
		}
		works[i].Diff_minute = strconv.Itoa(diff)
		sum += diff
	}

	return works, sum, nil
}

func (r RealRepo) GetSummary(auth0_id, report_date string) ([]WorkSummary, error) {
	rowsSum, err := r.DB.Query(`
    SELECT project_name, work_class, task_name,
      SUM(TIMESTAMPDIFF(MINUTE, start_time, end_time)) as total_minute
    FROM workentries w
    JOIN dailyreports d ON d.report_id = w.report_id
    WHERE d.auth0_id = ? 
      AND d.report_date = ?
      AND task_name != '昼休み'
      AND end_time IS NOT NULL
    GROUP BY project_name, work_class, task_name
    ORDER BY MIN(start_time)
	`, auth0_id, report_date)

	if err != nil {
		return nil, fmt.Errorf("get works: %w", err)
	}
	defer rowsSum.Close()

	var summaries []WorkSummary

	for rowsSum.Next() {
		var s WorkSummary

		err := rowsSum.Scan(
			&s.Project_name,
			&s.Work_class,
			&s.Task_name,
			&s.Total_minute,
		)
		if err != nil {
			return nil, fmt.Errorf("get works: %w", err)
		}

		summaries = append(summaries, s)
	}

	return summaries, nil
}

func (r RealRepo) CreateWork(auth0_id string, req reportRequestBody) error {
	// トランザクションの開始
	tx, err := r.DB.Begin()
	if err != nil {
		return fmt.Errorf("get works: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		DELETE dr
		FROM dailyreports dr
		WHERE dr.auth0_id = ?
			AND dr.report_date < DATE_SUB(?, INTERVAL 1 MONTH)
	`, auth0_id, req.Date)
	if err != nil {
		return fmt.Errorf("delete old dailyreports: %w", err)
	}

	// DailyReportsにあるならそのidを取得、ないなら新規追加
	_, err = tx.Exec(`
    INSERT INTO dailyreports (auth0_id, report_date, remarks)
    VALUES (?, ?, ?)
    ON DUPLICATE KEY UPDATE
    report_id = LAST_INSERT_ID(report_id),
    remarks = VALUES(remarks)
	`, auth0_id, req.Date, req.Remarks)

	if err != nil {
		return fmt.Errorf("get works: %w", err)
	}

	var report_id int
	err = tx.QueryRow("SELECT LAST_INSERT_ID()").Scan(&report_id)

	// 開始を押したとき、まだendしてないもの（end_time=NULL）があればエラー
	var count int
	err = tx.QueryRow(`
    SELECT COUNT(*)
    FROM workentries w
		JOIN dailyreports d ON w.report_id = d.report_id
    WHERE w.end_time IS NULL AND d.auth0_id = ?
	`, auth0_id).Scan(&count)

	if count > 0 {
		return fmt.Errorf("すでに作業中です")
	}

	// report_idと紐づけてworkをinesrt
	var end_time *string // end_timeはnilで初期化する
	if req.Works.End_time == "" {
		end_time = nil
	} else {
		end_time = &req.Works.End_time
	}
	_, err = tx.Exec(`
    INSERT INTO workentries
    (report_id, project_name, work_class, task_name, start_time, end_time, memo)
    VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		report_id,
		req.Works.Project_name,
		req.Works.Work_class,
		req.Works.Task_name,
		req.Works.Start_time,
		end_time,
		req.Works.Memo,
	)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create works: %w", err)
	}
	return nil
}

func (r RealRepo) EndWork(auth0_id string, req reportRequestBody) error {
	// report_idと紐づけてworkのend_timeのみをUPDATE
	_, err := r.DB.Exec(`
    UPDATE workentries w
    JOIN dailyreports d ON d.report_id = w.report_id
		SET w.end_time = ?
		WHERE d.auth0_id = ?
			AND d.report_date = ?
			AND w.end_time IS NULL
	`, req.Works.End_time, auth0_id, req.Date)
	if err != nil {
		return fmt.Errorf("end works: %w", err)
	}
	return nil
}

func (r RealRepo) UpdateWork(auth0_id string, req Works, work_id string) error {
	// report_idと紐づけてUPDATE
	result, err := r.DB.Exec(`
		UPDATE workentries w
		JOIN dailyreports d ON w.report_id
		SET project_name = ?, work_class = ?, task_name = ?, start_time = ?, end_time = ?
		WHERE w.work_entry_id = ? AND d.auth0_id = ?	
	`, req.Project_name, req.Work_class, req.Task_name, req.Start_time, req.End_time, work_id, auth0_id)

	if err != nil {
		return fmt.Errorf("update works: %w", err)
	}

	// 該当行がないならエラーにする
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("Not found this work")
	}

	return nil
}

func (r RealRepo) DeleteWork(auth0_id string, work_id string) error {
	result, err := r.DB.Exec(`
		DELETE w
		FROM workentries w
		JOIN dailyreports d ON w.report_id = d.report_id
		WHERE work_entry_id = ? AND d.auth0_id = ?
	`, work_id, auth0_id)

	if err != nil {
		return fmt.Errorf("update works: %w", err)
	}

	// 該当行がないならエラーにする
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("Not found this work")
	}

	return nil
}

// User関係のinterface
type UserRepository interface {
	SaveUserToDB() error
}

func (r RealRepo) SaveUserToDB(c *gin.Context) {
	session := sessions.Default(c)
	auth0_id := session.Get("auth0_id")
	profile := session.Get("profile").(map[string]interface{})
	name := profile["name"].(string)
	email := profile["email"].(string)

	_, err := r.DB.Exec(`
    INSERT INTO Users (auth0_id, name, email)
    VALUES (?, ?, ?)
    ON DUPLICATE KEY UPDATE
    name = VALUES(name),
		email = VALUES(email);
	`, auth0_id, name, email)

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
}

/*
func GetMSToken(auth0_id string) (string, string, error) {
	var ms_access_token string
	var ms_refresh_token string

	err := DB.QueryRow(`
    SELECT ms_access_token, ms_refresh_token
    FROM users
    WHERE auth0_id = ?
`, auth0_id).Scan(&ms_access_token, &ms_refresh_token)

	if err != nil {
		return "", "", fmt.Errorf("get works: %w", err)
	}

	return ms_access_token, ms_refresh_token, nil
}

func UpdateMSToken(auth0_id, ms_access_token, ms_refresh_token string) error {
	_, err := DB.Exec(`
		UPDATE users
		SET ms_access_token = ?, ms_refresh_token = ?
		WHERE auth0_id = ?
	`, ms_access_token, ms_refresh_token, auth0_id)

	if err != nil {
		return fmt.Errorf("update works: %w", err)
	}

	return nil
}
*/
