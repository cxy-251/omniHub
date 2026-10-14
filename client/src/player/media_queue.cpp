#include "media_queue.h"
#include "media_address.h"

bool MediaQueue::replace(const QStringList &sources, int index) {
    QStringList entries;
    qsizetype bytes = 0;
    auto fail = [this] { m_error = tr("播放列表最多 256 项、地址共 1MiB，且需有效媒体地址和位置"); emit errorChanged(); return false; };
    if (sources.size() > 256 || (sources.isEmpty() ? index != -1 && index != 0 : index < 0 || index >= sources.size())) return fail();
    for (const auto &source : sources) {
        const auto address = mediaAddress(source);
        if (!address) return fail();
        bytes += address->toUtf8().size();
        if (bytes > 1024 * 1024) return fail();
        entries.append(*address);
    }
    m_entries = std::move(entries);
    m_index = m_entries.isEmpty() ? -1 : index;
    if (!m_error.isEmpty()) { m_error.clear(); emit errorChanged(); }
    emit changed();
    return true;
}
bool MediaQueue::select(int index) {
    if (index < 0 || index >= m_entries.size()) return false;
    m_index = index; emit changed(); return true;
}
bool MediaQueue::next() { return select(m_index + 1); }
bool MediaQueue::previous() { return select(m_index - 1); }
