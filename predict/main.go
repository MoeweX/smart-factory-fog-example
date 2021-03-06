package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sajari/regression"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var prognosisEndpoint string = fmt.Sprintf("http://%s:%s/input", os.Getenv("PROGNOSIS_IP"), os.Getenv("PROGNOSIS_PORT"))

// amount of historic data to save
const historic int = 1000

type PackCtrlData struct {
	Rate    int    `json:"rate"`
	Backlog int    `json:"backlog"`
	UUID    string `json:"uuid"`
}

type Store struct {
	Data    []PackCtrlData
	Current int
	sync.Mutex
}

var s Store

func update(d PackCtrlData) {

	data := make([]PackCtrlData, historic)

	s.Lock()
	s.Current = (s.Current + 1) % historic

	s.Data[s.Current] = d

	c := s.Current
	copy(s.Data, data)
	s.Unlock()

	p := "Predictions:\n"
	id := d.UUID

	for i := 0; i < 5; i++ {
		r := new(regression.Regression)
		r.SetObserved("Production Rate")
		r.SetVar(0, "Index")
		r.SetVar(1, "Backlog")

		for j, entry := range data {
			r.Train(regression.DataPoint(float64(entry.Rate), []float64{float64((c + j) % historic), float64(entry.Backlog)}))
		}

		err := r.Run()

		if err != nil {
			log.Print(err)
			return
		}

		for j := 0; j < historic; j++ {
			pred, err := r.Predict([]float64{float64(historic + j), rand.Float64()})

			if err != nil {
				log.Print(err)
				return
			}

			p = fmt.Sprintf("%s\n%#v", p, pred)
		}
	}

	type Prediction struct {
		Prediction string `json:"prediction"`
		UUID       string `json:"uuid"`
	}

	text, err := json.Marshal(Prediction{
		Prediction: p,
		UUID:       id,
	})

	if err != nil {
		log.Print(err)
		return
	}

	log.Printf("send,predict,%s,%s", id, strconv.FormatInt(time.Now().UnixNano(), 10))
	req, err := http.NewRequest("POST", prognosisEndpoint, bytes.NewReader(text))

	if err != nil {
		log.Print(err)
		return
	}

	_, err = (&http.Client{}).Do(req)

	if err != nil {
		log.Print(err)
		return
	}
}

func main() {
	// HTTP service, collects historic data as well and sends it out to a frontend if requested

	s = Store{
		Data:    make([]PackCtrlData, historic),
		Current: 0,
	}

	// fill Store with random data first but mark it as invalid in case it is ever logged somewhere

	for i := range s.Data {
		s.Data[i] = PackCtrlData{
			Rate:    rand.Intn(1000),
			Backlog: rand.Intn(1000),
			UUID:    "invalid",
		}

	}

	http.HandleFunc("/input", func(w http.ResponseWriter, r *http.Request) {
		timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)

		var d PackCtrlData
		err := json.NewDecoder(r.Body).Decode(&d)

		if err != nil {
			return
		}

		log.Printf("recv,input,%s,%s", d.UUID, timestamp)

		go update(d)

	})

	log.Fatal(http.ListenAndServe(":"+os.Getenv("PREDICT_PORT"), nil))
}
