# 01 — Tổng quan

## Mục tiêu

Viết một key-value database từ con số không bằng Go, để hiểu thứ mà bình thường ta chỉ
gọi API: dữ liệu nằm ở đâu, làm sao nó sống sót qua một lần tắt máy, làm sao nhiều
client cùng ghi mà không giẫm lên nhau.

Ưu tiên là hiểu rõ từng lớp, không phải chạy nhanh. Mỗi bước làm nhỏ, có test, có ghi chú
lý do — rồi mới sang bước sau.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời, chạy vài lệnh Set/Get rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # [02] KV trong RAM: map + RWMutex
    └── wal/                   # [03] định dạng record cho write-ahead log
```

`internal/` là quy ước của Go: package nằm trong đó chỉ import được từ trong chính module
này. Nó giữ cho phần bên trong được tự do thay đổi — chưa có ai ngoài repo phụ thuộc vào
nó để mà làm hỏng.

## Cách chạy

```sh
make run     # chạy demo trong main.go
make test    # chạy toàn bộ test
make check   # fmt + vet + test — chạy cái này trước khi commit
make help    # xem tất cả target
```

Xem thêm `make cover` (báo cáo coverage dạng HTML) và `make test-race`
(cần CGO và một trình biên dịch C; hiện chưa chạy được trên máy dev vì thiếu gcc).

## Đã có gì

| Phần | Trạng thái |
|---|---|
| KV in-memory, an toàn đa luồng | xong — [02](02-in-memory-store.md) |
| Serialize entry thành byte | xong — [03](03-serialization.md) |
| Ghi log xuống đĩa, replay khi khởi động | chưa |
| Server + giao thức để client kết nối | chưa |
| Compaction, on-disk format, index | chưa |

## Lộ trình

1. **Append-only log.** Mỗi lệnh ghi được nối vào cuối một file trước khi áp vào memory.
   Khởi động lại thì đọc lại log từ đầu để dựng lại state. Ghi nối đuôi nhanh vì đĩa
   không phải tìm chỗ, và một bản ghi dở dang luôn nằm ở cuối file nên phát hiện được.
   Định dạng record cho bước này đã xong ở [03](03-serialization.md).
2. **Server + giao thức.** Cho client nói chuyện với db qua network, thay vì chỉ gọi hàm
   trong cùng process.
3. **Compaction.** Log chỉ nối thêm nên nó phình mãi; key ghi đè 1000 lần thì có 1000 bản
   ghi mà chỉ 1 bản còn giá trị. Cần dọn định kỳ.
4. **On-disk format + index.** Bỏ giả định "toàn bộ dữ liệu vừa trong RAM".

## Giới hạn hiện tại

Nói thẳng: hiện tại đây **chưa phải một database**. Nó là một `map` có khóa và một hàm
mã hóa. Tắt chương trình là mất sạch dữ liệu, và không có cách nào kết nối từ bên ngoài.
Hai điều đó được xử lý ở bước 1 và 2 của lộ trình.
