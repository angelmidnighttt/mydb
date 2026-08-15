# mydb

Một key-value store viết từ đầu bằng Go, làm để học cách một database hoạt động bên dưới.

**Trạng thái:** mới có tầng lưu trữ in-memory. Chưa có persistence, chưa có network layer.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời, chạy vài lệnh Set/Get rồi thoát
├── Makefile                   # build / test / run
└── internal/store/
    ├── store.go               # in-memory KV: map + RWMutex
    ├── store_test.go
    └── example_test.go        # ví dụ dùng, hiện trong godoc
```

## Chạy thử

```sh
make run     # chạy demo trong main.go
make test    # chạy toàn bộ test
make check   # fmt + vet + test
make help    # xem tất cả target
```

## Package `store`

```go
import "github.com/angelmidnighttt/mydb/internal/store"

s := store.New()
s.Set("hello", []byte("world"))
v, ok := s.Get("hello")   // []byte("world"), true
```

| Hàm | Trả về | Ghi chú |
|---|---|---|
| `New()` | `*Store` | store rỗng, dùng được ngay, không cần init thêm |
| `Get(key)` | `([]byte, bool)` | `ok = false` nếu key không tồn tại; value trả về là **bản copy** |
| `Set(key, value)` | — | ghi đè nếu key đã có; value được **copy** khi lưu |
| `Delete(key)` | `bool` | `true` nếu key có tồn tại trước khi xóa |
| `Len()` | `int` | số key hiện tại |
| `Keys()` | `[]string` | tất cả key, **thứ tự không xác định** |

Mọi method đều an toàn khi gọi từ nhiều goroutine.

## Ghi chú thiết kế

**Value được copy ở cả hai chiều.** `Set` copy slice của caller trước khi lưu, `Get` trả về copy chứ không trả slice gốc. Nếu không làm vậy thì store và caller cùng trỏ vào một mảng byte: caller sửa slice sau khi `Set`, hoặc sửa slice vừa `Get` về, đều âm thầm làm hỏng dữ liệu trong store — một lỗi rất khó lần ra. Đổi lại là mỗi lần đọc/ghi tốn một lần cấp phát; chấp nhận được ở giai đoạn này. `store_test.go:TestValueIsCopied` khóa hành vi này lại.

**Dùng `sync.RWMutex` thay vì `sync.Mutex`.** KV store đọc nhiều hơn ghi, `RWMutex` cho phép nhiều `Get` chạy song song, chỉ chặn nhau khi có `Set`/`Delete`.

**`map[string][]byte` chứ không phải `map[string]string`.** Value của database là byte thô — có thể là số, struct đã serialize, ảnh — không phải lúc nào cũng là chuỗi UTF-8 hợp lệ. Dùng `[]byte` từ đầu để sau này không phải sửa lại kiểu khi thêm persistence.

## Giới hạn hiện tại

- **Mất sạch dữ liệu khi tắt chương trình** — chưa có WAL hay snapshot.
- **Chỉ chạy trong một process** — chưa có server, chưa nói chuyện được qua network.
- Chưa có TTL / expire, chưa có transaction, chưa có range scan.
- `Keys()` copy toàn bộ danh sách key và giữ lock trong lúc đó — sẽ thành vấn đề khi số key lớn.
- Không giới hạn bộ nhớ: ghi tới đâu RAM ăn tới đó, không có eviction.

## Kiểm thử

```sh
make test        # chạy được ngay
make test-race   # cần CGO + trình biên dịch C (gcc) trong PATH
```

`make test-race` hiện fail trên máy dev vì không có gcc. `TestConcurrentAccess` vẫn chạy trong `make test`
nhưng chỉ thực sự phát hiện được data race khi có `-race`, nên cài MinGW-w64/TDM-GCC nếu muốn kiểm tra
phần khóa cho nghiêm túc.

## Hướng đi tiếp

1. Giao thức + server để client kết nối được từ ngoài.
2. Persistence: append-only log, khởi động lại thì replay để dựng lại state.
3. Compaction cho log, rồi tính tới on-disk format.
