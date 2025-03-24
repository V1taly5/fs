package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

type AutoCompleteTerminal struct {
	RootCommand    *cobra.Command
	History        []string
	MaxHistorySize int
	oldState       *term.State
}

func NewAutoCompleteTerminal(rootCmd *cobra.Command, maxHistiorySize int) *AutoCompleteTerminal {
	return &AutoCompleteTerminal{
		RootCommand:    rootCmd,
		History:        []string{},
		MaxHistorySize: maxHistiorySize,
	}
}

func (t *AutoCompleteTerminal) handleAutocomplete(line []rune, position int) ([]rune, int) {
	// логика автодополнения
	completions := t.getCompletions(string(line))
	if len(completions) <= 0 {
		return line, position
	}
	if len(completions) == 1 {
		// если есть только один вариант, автоматически дополняем
		newLine := t.completeCommand(string(line), completions[0])
		line = []rune(newLine)
		position = len(line)
		// перерисовываем строку
		fmt.Print("\r> ", string(line))
	} else {
		// интерактивный выбор из вариантов
		selectedCompletion := t.showInteractiveCompletions(completions)
		if selectedCompletion != "" {
			newLine := t.completeCommand(string(line), selectedCompletion)
			line = []rune(newLine)
			position = len(line)
			// перерисовываем строку
			fmt.Print("\r> ", string(line))
		}
	}
	return line, position
}

func (t *AutoCompleteTerminal) Start(ctx context.Context, cancelFunc context.CancelFunc) error {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	t.oldState = oldState
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	var line []rune
	var position int
	var historyPos int

	fmt.Print("> ")

	buf := make([]byte, 3)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n CLI is shutting down gracefully.")
			return nil
		default:
			_, err := os.Stdin.Read(buf)
			if err != nil {
				return err
			}
			switch {
			case buf[0] == 3: // Ctrl+C
				fmt.Println("\nВыход")
				cancelFunc()
				return nil

			case buf[0] == 9: // Tab
				// логика автодополнения
				line, position = t.handleAutocomplete(line, position)

			case buf[0] == 13: // Enter
				fmt.Println()
				command := string(line)
				if command != "" {
					// добавляем команду в историю
					t.addToHistory(command)
					// восстанавливаем состояние терминала для выполнения команды
					term.Restore(int(os.Stdin.Fd()), t.oldState)

					// разделяем команду и аргументы
					parts := strings.Fields(command)
					args := append([]string{os.Args[0]}, parts...)

					// сохраняем оригинальные аргументы
					originalArgs := os.Args
					os.Args = args

					// выполняем команду через Cobra
					err := t.RootCommand.Execute()
					if err != nil {
						fmt.Println(err)
					}

					// восстанавливаем оригинальные аргументы
					os.Args = originalArgs

					// возвращаемся в raw mode
					var rawErr error
					t.oldState, rawErr = term.MakeRaw(int(os.Stdin.Fd()))
					if rawErr != nil {
						return rawErr
					}
				}

				// сбрасываем строку и выводим новое приглашение
				line = []rune{}
				position = 0
				historyPos = len(t.History)
				fmt.Print("> ")

			case buf[0] == 27 && buf[1] == 91: // стрелки
				if buf[2] == 65 { // стрелка вверх
					if historyPos > 0 {
						historyPos--
						line = []rune(t.History[historyPos])
						position = len(line)
						fmt.Print("\r> ", string(line), strings.Repeat(" ", 10), "\r> ", string(line))
					}
				} else if buf[2] == 66 { // стрелка вниз
					if historyPos < len(t.History) {
						historyPos++
						if historyPos == len(t.History) {
							line = []rune{}
						} else {
							line = []rune(t.History[historyPos])
						}
						position = len(line)
						fmt.Print("\r> ", string(line), strings.Repeat(" ", 10), "\r> ", string(line))
					}
				} else if buf[2] == 67 { // стрелка вправо
					if position < len(line) {
						position++
						fmt.Print("\r> ", string(line[:position]))
					}
				} else if buf[2] == 68 { // стрелка влево
					if position > 0 {
						position--
						fmt.Print("\r> ", string(line[:position]))
					}
				}

			case buf[0] == 127: // backspace
				if position > 0 {
					// удаляем символ
					if position < len(line) {
						line = append(line[:position-1], line[position:]...)
					} else {
						line = line[:position-1]
					}
					position--
					// перерисовываем строку
					fmt.Print("\r> ", string(line), " ", "\r> ", string(line[:position]))
				}

			default:
				// добавляем символ в текущую позицию
				if position == len(line) {
					line = append(line, rune(buf[0]))
				} else {
					line = append(line[:position+1], line[position:]...)
					line[position] = rune(buf[0])
				}
				position++
				fmt.Print("\r> ", string(line[:position]))
			}

			// очищаем буфер
			buf[0], buf[1], buf[2] = 0, 0, 0
		}
	}

}

// attachCommand добавляет команды к корневой команде
func AttachCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}

func CliStart(ctx context.Context, args []string, appCtx *AppContext) {

	AttachCommand(createEchoCommand(appCtx))
	AttachCommand(createFileSendingCommand(appCtx))
	AttachCommand(createListPeersCommand(appCtx))
	AttachCommand(createConnectPeerCommand(appCtx))
	AttachCommand(createIndexSendingCommand(appCtx))
	AttachCommand(createSimpleEchoCommand(appCtx))

	if len(args) > 1 {
		if err := Execute(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	} else {
		// создаем и запускаем интерактивный терминал с автодополнением
		terminal := NewAutoCompleteTerminal(rootCmd, 100)
		fmt.Println("Интерактивный терминал с автодополнением")
		fmt.Println("Нажмите Tab для автодополнения и выбора вариантов, Ctrl+C для выхода")

		if err := terminal.Start(ctx, appCtx.CancelFunc); err != nil {
			fmt.Printf("Ошибка при запуске терминала: %v\n", err)
			os.Exit(1)
		}
	}
}

var rootCmd = &cobra.Command{
	Use:   "service-cli",
	Short: "CLI для взаимодействия c сервисом",
	Long:  `Это CLI-интерфейс для взаимодействия с сервисом через команды.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Введите команду для взаимодействия с основным сервисом.")
	},
}

// Execute запускает корневую команду
func Execute() error {
	return rootCmd.Execute()
}

// showInteractiveCompletions показывает варианты автодополнения с возможностью выбора
func (t *AutoCompleteTerminal) showInteractiveCompletions(completions []string) string {
	fmt.Print("\n")

	// выводим варианты в столбик с номерами
	row, col := getCursorPosition()
	printOptions(completions, row+1, col)

	// cохраняем текущее состояние, чтобы временно выйти из raw mode
	// для более удобного ввода выбора
	term.Restore(int(os.Stdin.Fd()), t.oldState)

	// приглашение для выбора
	fmt.Print("\nВыберите вариант (1-", len(completions), ") или нажмите Enter для отмены: ")

	// считываем выбор пользователя
	var choice int
	var input string
	fmt.Scanln(&input)

	// преобразуем ввод в число, если это возможно
	if input != "" {
		fmt.Sscanf(input, "%d", &choice)
	}

	// возвращаемся в raw mode
	var err error
	t.oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println("Ошибка при возврате в raw mode:", err)
		return ""
	}

	// проверяем выбор
	if choice > 0 && choice <= len(completions) {
		fmt.Print("> ")
		return completions[choice-1]
	}

	// если выбор недействителен, возвращаем пустую строку
	fmt.Print("> ")
	return ""
}

// addToHistory добавляет команду в историю
func (t *AutoCompleteTerminal) addToHistory(command string) {
	// Проверяем, не повторяется ли команда
	if len(t.History) > 0 && t.History[len(t.History)-1] == command {
		return
	}

	t.History = append(t.History, command)
	// ограничиваем размер истории
	if len(t.History) > t.MaxHistorySize {
		t.History = t.History[1:]
	}
}

// getCompletions возвращает возможные варианты автодополнения
func (t *AutoCompleteTerminal) getCompletions(input string) []string {
	if input == "" {
		// Если ввод пустой, предлагаем все команды верхнего уровня
		return getCobraCommands(t.RootCommand)
	}

	parts := strings.Fields(input)

	// если введена только часть команды
	if len(parts) == 1 {
		return getMatchingCommands(t.RootCommand, parts[0])
	}

	// для более сложных случаев с флагами и значениями
	// пытаемся найти команду и ее флаги
	cmd, _, lastPart := findCobraCommandAndFlags(t.RootCommand, parts)
	if cmd != nil {
		// Если последний введенный символ - дефис, предлагаем флаги
		if strings.HasPrefix(lastPart, "-") {
			return getMatchingFlags(cmd, lastPart)
		}

		// попытаемся определить, завершаем ли мы значение флага
		if len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-2], "-") {
			// TODO: Здесь можно добавить логику для получения возможных значений флагов
			// Это потребует дополнительной структуры данных или метаданных
			return []string{}
		}

		// предлагаем подкоманды, если они есть
		if len(cmd.Commands()) > 0 {
			return getMatchingCommands(cmd, lastPart)
		}
	}

	return []string{}
}

// getCobraCommands возвращает все команды верхнего уровня
func getCobraCommands(rootCmd *cobra.Command) []string {
	var result []string
	for _, cmd := range rootCmd.Commands() {
		if !cmd.Hidden {
			result = append(result, cmd.Name())
		}
	}
	return result
}

// getMatchingCommands возвращает команды, начинающиеся с prefix
func getMatchingCommands(parentCmd *cobra.Command, prefix string) []string {
	var matches []string
	for _, cmd := range parentCmd.Commands() {
		if !cmd.Hidden && strings.HasPrefix(cmd.Name(), prefix) {
			matches = append(matches, cmd.Name())
		}
	}
	return matches
}

// getMatchingFlags возвращает флаги, начинающиеся с prefix
func getMatchingFlags(cmd *cobra.Command, prefix string) []string {
	var matches []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// Проверяем короткое имя флага
		if len(flag.Shorthand) > 0 {
			shortFlag := "-" + flag.Shorthand
			if strings.HasPrefix(shortFlag, prefix) {
				matches = append(matches, shortFlag)
			}
		}

		// Проверяем полное имя флага
		fullFlag := "--" + flag.Name
		if strings.HasPrefix(fullFlag, prefix) {
			matches = append(matches, fullFlag)
		}
	})
	return matches
}

// findCobraCommandAndFlags находит команду и ее флаги по введенной строке
func findCobraCommandAndFlags(rootCmd *cobra.Command, parts []string) (*cobra.Command, []string, string) {
	cmd := rootCmd
	cmdPath := []string{}
	var i int

	// проходимся по частям введенной команды, пока находим подходящие подкоманды
	for i = 0; i < len(parts); i++ {
		part := parts[i]

		// если часть начинается с "-", это флаг
		if strings.HasPrefix(part, "-") {
			break
		}

		found := false
		for _, subCmd := range cmd.Commands() {
			if subCmd.Name() == part {
				cmd = subCmd
				cmdPath = append(cmdPath, part)
				found = true
				break
			}
		}

		if !found {
			break
		}
	}

	// остальные части - флаги и их значения
	flags := parts[i:]
	lastPart := ""
	if len(parts) > 0 {
		lastPart = parts[len(parts)-1]
	}

	return cmd, flags, lastPart
}

// completeCommand дополняет введенную строку выбранным вариантом
func (t *AutoCompleteTerminal) completeCommand(input, completion string) string {
	if input == "" {
		return completion
	}

	parts := strings.Fields(input)

	// если дополняем первую часть (имя команды)
	if len(parts) == 1 && !strings.HasSuffix(input, " ") {
		return completion
	}

	// если дополняем флаг или значение флага
	newInput := strings.Join(parts[:len(parts)-1], " ") + " " + completion
	return newInput
}

func getCursorPosition() (int, int) {
	fmt.Print("\033[6n")

	var row, cal int

	fmt.Scanf("\033[%d;%dR", &row, &cal)
	return row, cal
}

func printOptions(options []string, startRow, startCol int) {
	for i, option := range options {
		fmt.Printf("\033[%d;%dH%d. %s\n", startRow+i, startCol, i+1, option)
	}
}
