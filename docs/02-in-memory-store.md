# 02 — In-memory store

Package: [`internal/store`](../internal/store/store.go)

> **Cập nhật ở [11](11-sorted-store.md):** `map` bên trong đã được thay bằng hai mảng
> sắp xếp cộng binary search, và key đổi từ `string` sang `[]byte`. Mọi quyết định thiết
> kế bên dưới vẫn còn nguyên giá trị — copy hai chiều, `RWMutex`, `[]byte` cho value —
> chỉ có cấu trúc dữ liệu bên dưới là khác. Chỗ nào nói "map" thì đọc là "cái đã từng là
> map".

## Mục tiêu

Lớp lưu trữ đơn giản nhất có thể: giữ key-value trong RAM, cho nhiều goroutine dùng chung
mà không sinh ra data race. Đây là nền để các lớp sau (log, server) đứng lên — chúng sẽ
ghi xuống đĩa rồi áp thay đổi vào chính cái store này.

## API

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
| `Has(key)` | `bool` | chỉ kiểm tra tồn tại, không copy value |
| `Set(key, value)` | — | ghi đè nếu key đã có; value được **copy** khi lưu |
| `Delete(key)` | `bool` | `true` nếu key có tồn tại trước khi xóa |
| `Len()` | `int` | số key hiện tại |
| `Keys()` | `[]string` | tất cả key, **theo thứ tự** kể từ [11](11-sorted-store.md) |

Mọi method đều an toàn khi gọi từ nhiều goroutine.

## Quyết định thiết kế

### Value được copy ở cả hai chiều

`Set` copy slice của caller trước khi lưu, `Get` trả về copy chứ không trả slice gốc.

Lý do: slice trong Go chỉ là con trỏ tới một mảng byte. Nếu lưu thẳng slice của caller
thì store và caller cùng trỏ vào một mảng — caller sửa slice đó sau khi `Set`, hoặc sửa
slice vừa `Get` về, đều âm thầm làm hỏng dữ liệu bên trong store:

```go
v := []byte("value")
s.Set("k", v)
v[0] = 'X'          // nếu không copy: dữ liệu trong store thành "Xalue"
```

Loại lỗi này không crash, không báo gì cả, chỉ làm dữ liệu sai ở một chỗ rất xa nơi gây
ra nó. Cái giá phải trả là mỗi lần đọc/ghi tốn một lần cấp phát — chấp nhận được ở giai
đoạn này, và có thể tính lại sau nếu đo được là nút thắt.

`TestValueIsCopied` khóa hành vi này lại theo cả hai chiều.

### `sync.RWMutex` thay vì `sync.Mutex`

KV store đọc nhiều hơn ghi rất nhiều. `RWMutex` cho phép nhiều `Get` chạy song song và
chỉ chặn khi có `Set`/`Delete`. Với `Mutex` thì hai goroutine chỉ đọc thôi cũng phải xếp
hàng chờ nhau.

Lưu ý: mọi method đều `defer Unlock()`. Điều đó khiến lock được nhả kể cả khi hàm return
sớm hay panic — quan trọng hơn ta tưởng, vì một lock bị giữ vĩnh viễn sẽ treo toàn bộ
chương trình chứ không chỉ hỏng một request.

### `map[string][]byte` chứ không phải `map[string]string`

Value của database là byte thô: có thể là số, struct đã serialize, ảnh — không nhất
thiết là chuỗi UTF-8 hợp lệ. Dùng `[]byte` từ đầu để khi thêm persistence không phải sửa
lại kiểu dữ liệu xuyên suốt.

Key vẫn là `string` vì Go chỉ cho phép dùng kiểu so sánh được làm map key — `[]byte` thì
không. Đây là ràng buộc của ngôn ngữ, không phải lựa chọn.

## Giới hạn hiện tại

- **Bản thân store không chạm tới đĩa** — nó chỉ là RAM. Việc giữ dữ liệu qua các lần
  khởi động do [`internal/kv`](04-write-ahead-log.md) lo, bằng cách ghi log rồi replay
  vào chính store này.
- **Chỉ chạy trong một process.** Chưa có server.
- Chưa có TTL/expire, chưa có transaction. Range scan thì cấu trúc đã sẵn sàng từ
  [11](11-sorted-store.md), nhưng chưa có API để duyệt.
- `Keys()` giữ read lock trong lúc copy toàn bộ danh sách key — với vài triệu key thì
  mỗi lần gọi sẽ chặn writer một khoảng đáng kể.
- **Không giới hạn bộ nhớ.** Ghi tới đâu RAM ăn tới đó, không có eviction. Client ghi đủ
  nhiều là process bị OOM kill.
