package persist

import "github.com/liza-mas/liza/internal/db"

func persistState(statePath, transcript string) error {
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		return err
	}
	if transcript != "" {
		state.Tasks[0].Extra["summary"] = transcript
	}
	return bb.Write(state)
}
