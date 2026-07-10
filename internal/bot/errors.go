package bot

import "github.com/andatoshiki/omni/internal/command"

const errorDetailLimit = command.ErrorDetailLimit

func errorMessage(err error) string {
	return command.ErrorMessage(err)
}
