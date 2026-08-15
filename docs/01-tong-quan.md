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
├── main.go                    # demo tạm thời: mở db, ghi một key rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # [02] KV trong RAM: map + RWMutex
    ├── wal/                   # [03] định dạng record  [04] file log append-only
    └── kv/                    # [04] ghép log với store: ghi log trước, replay khi mở
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
| Ghi log xuống đĩa, replay khi khởi động | xong — [04](04-write-ahead-log.md) |
| fsync: ghi thành công là chắc chắn không mất | xong — [04](04-write-ahead-log.md#đảm-bảo-dữ-liệu-thật-sự-xuống-đĩa) |
| Chịu được log cắt cụt do mất điện | chưa |
| Server + giao thức để client kết nối | chưa |
| Compaction, on-disk format, index | chưa |

## Lộ trình

1. **Chống hỏng log.** Thêm checksum cho từng record, và khi khởi động gặp record dở dang
   ở cuối file thì cắt bỏ rồi chạy tiếp thay vì từ chối mở. Hiện tại một lần mất điện
   đúng lúc ghi là database không lên lại được — xem phần giới hạn của
   [04](04-write-ahead-log.md).
2. **Server + giao thức.** Cho client nói chuyện với db qua network, thay vì chỉ gọi hàm
   trong cùng process.
3. **Compaction.** Log chỉ nối thêm nên nó phình mãi; key ghi đè 1000 lần thì có 1000 bản
   ghi mà chỉ 1 bản còn giá trị. Cần dọn định kỳ.
4. **On-disk format + index.** Bỏ giả định "toàn bộ dữ liệu vừa trong RAM".

## Giới hạn hiện tại

Ghi đã thật sự xuống đĩa (fsync), nhưng vẫn còn hai lỗ hổng lớn: **chưa kết nối được từ
bên ngoài** (chưa có server), và **chưa chịu nổi một record ghi dở** — mất điện đúng lúc
đang ghi thì lần khởi động sau không mở được database. Bước 1 và 2 của lộ trình xử lý đúng
hai chuyện này.

Đổi lại, tốc độ ghi hiện rất thấp (~2.900 ghi/giây) vì fsync sau mỗi record. Đó là lựa
chọn có chủ ý: an toàn trước, nhanh sau.
