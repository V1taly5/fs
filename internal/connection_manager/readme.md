# Connection manager 
`Connection manager` — модуль для управления соединениями с узлами сети, информация о которых храниться в SQLite базе данных. Модуль позволяет устанавливать соединения с узлами по запросу пользователя, используя различные протаколы связи (TCP, QUIC...), иноформация о которых храниться в таблице Endpoints.
## Components 
1. ConnectionManager
```go
type ConnectionManager struct {
    node *node.Node
    db *storae.nodestorage
    log *slog.Logger
    config *ConnectionConfig
    activeConns *sync.map
    connFactory *sync.map
    ctx context.Context
    cancel context.CencelFunc
}
```

Configuration 

```go
type ConnectionConfig struct {
    ConnectionTimeout time.Duration
    BaseRetryInterval time.Duration
    MaxRetryInterval time.Duration
    MaxRetryCount int
}
```

2. ConnectionFactory
Фабрика для создания соединений различного типа.

```go
type ConnectionFactory struct {
    connection map[string]Connector
    node *node.Node
    log *slog.Logger
}
```

3. Connector (Interface)
Интерфейс для различных типов соединений.
```go
type Connector interface {
    Connect(ctx context.Context, endpoint storage.Endpoint) (Connection, error)
    SupportProtocol(protocol string) bool
    Name() string
}
```

4. Connection (Interface)
Интерфейс для работы с установленным соединением.
```go
type Connecton interface {
    NodeID() string
    Address() string
    Protocol() string
    Close() error
    IsActive() bool
}
```

## Основные методы для Connection manager
1. Initialization
2. Получение данных об узлах
    - GetAllNodes 
    ```go 
    func GetAllNodes() ([]storate.Node, error) 
    ```
    - GetNodeByID
     ```go 
    func GetNodeByID(nodeID string) (storage.Node, error)
    ```
    - GetEndpointsForNode
     ```go 
    func GetEndpointsForNode(nodeID string) ([]storate.Endpoints, error)
    ```
3. Управление соединениями
    - Подключение к узлу по указанному endpoints
    ```go
    func ConnectToEndpoint(
        ctx context.Context,
        endpoint storage.Endpoint,
        retryOptions *RetryOptions,
    ) (Connection, error)
    ```
    - Подключится к узлу, автоматически выбраб Endpoint
    ```go
    func ConnectToNode (
        ctx context.Context,
        nodeID string,
        preferredProtocol string,
        tetryOptions *RetryOptions,
    ) (Connection, error)
    ```
    - Повторная попытка подключится с экспоненциальной задержкой
    ```go
    func RetryConnection(
        ctx context.Context,
        endpoint storage.Endpoint,
        retryCount int,
    ) (Connection, error)
    ```
    - Отключиться от узла
    ```go
    func DisconnectNode(nodeID string) error
    ```
    - Получить список активных подключений
    ```go
    func GetActiveConnections() []Connection
    ```
4. Опции повторного подключения
```go
type RetryOptions struct{
    MaxAttempts int
    UseExponential bool
    InitialDelay time.Duration
    MaxDelay time.Duration
}
```

## Алгоритмы работы
### Подключение к узлу
1. Пользователь запрашивает список узлов через GetAllNodes() или конкретный узел через GetNodeByID()
2. Для выбранного узла получает доступные endpoints через GetEndpointsForNode()
3.Выбирает нужный endpoint или предоставляет предпочтительный протокол через ConnectToNode()
4. Подключение обрабатывается следующим образом: 
    - Проверяется наличие активного соединения
    - Выбирается подходящий connector через ConnectionFactory
    - Устанавливается соединение с заданным таймаутом
    - В случае успеха соединение добавляется в activeConns и обновляется статистика в БД
    - В случае неудачи возвращается ошибка, и если заданы параметры повторных попыток, планируется повторное соединение
### Выбор протокола соединения
1. Если пользователь указал конкретный endpoint, используется его протокол
2. Если пользователь указал предпочтительный протокол: 
    - Ищутся endpoints с указанным протоколом
    - При наличии нескольких, выбирается с лучшей статистикой успешных соединений
3. Если протокол не указан: 
    - Выбирается endpoint с наибольшим success_count и наименьшим failure_count
### Экспоненциальная задержка для повторных попыток
```go
    delay = BaseRetryInterval * math.Pow(2, float64(attemptCount-1))
    if delay > MaxRetryInterval {
        delay = MaxRetryInterval
    }
```