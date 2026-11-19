import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

// material-ui
import { useTheme } from '@mui/material/styles';
import useMediaQuery from '@mui/material/useMediaQuery';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Stack from '@mui/material/Stack';
import Alert from '@mui/material/Alert';
import Snackbar from '@mui/material/Snackbar';
import Tooltip from '@mui/material/Tooltip';
import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';

// icons
import AddIcon from '@mui/icons-material/Add';
import RefreshIcon from '@mui/icons-material/Refresh';
import CategoryIcon from '@mui/icons-material/Category';
import SearchIcon from '@mui/icons-material/Search';
import ClearIcon from '@mui/icons-material/Clear';

// project imports
import MainCard from 'ui-component/cards/MainCard';
import Pagination from 'components/Pagination';
import {
  getSubscriptions,
  addSubscription,
  updateSubscription,
  deleteSubscription,
  sortSubscription,
  batchSortSubscription,
  copySubscription,
  previewSubscriptionNodes
} from 'api/subscriptions';
import { getNodeCheckMeta } from 'api/nodeCheck';
import {
  getNodeSelector,
  getNodeSelectorByIds,
  getNodeGroupStats,
  getNodeCountries,
  getNodeGroups,
  getNodeSources,
  getNodeIds,
  getProtocolUIMeta
} from 'api/nodes';
import { getTemplates } from 'api/templates';
import { getScripts } from 'api/scripts';
import { getTags } from 'api/tags';
import { getAirports } from 'api/airports';
import { buildUnlockRulesPayload, normalizeUnlockRules, setUnlockMeta } from 'views/nodes/utils';
import { getRegisteredProtocolNames } from 'utils/protocolPresentation';
import { getNodeDisplayName } from 'utils/nodeDisplayName';

// components
import {
  ConfirmDialog,
  AccessLogsDialog,
  SubscriptionMobileCard,
  SubscriptionTable,
  SubscriptionFormDialog,
  ProxySubscriptionFormDialog,
  NodePreviewDialog,
  ChainProxyDialog,
  GroupSortDialog
} from './component';

// ==============================|| 节点池管理 ||============================== //

const createProxyFormData = () => ({
  name: '',
  description: '',
  AirportID: ''
});

export default function SubscriptionList({ mode = 'collection' }) {
  const theme = useTheme();
  const { t } = useTranslation();
  const matchDownMd = useMediaQuery(theme.breakpoints.down('md'));

  const [subscriptions, setSubscriptions] = useState([]);
  const [templates, setTemplates] = useState([]);
  const [scripts, setScripts] = useState([]);
  const [loading, setLoading] = useState(false);

  // 确认对话框
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmInfo, setConfirmInfo] = useState({
    title: '',
    content: '',
    action: null
  });

  const openConfirm = (title, content, action) => {
    setConfirmInfo({ title, content, action });
    setConfirmOpen(true);
  };

  const handleConfirmClose = () => {
    setConfirmOpen(false);
  };

  const handleConfirmAction = async () => {
    if (confirmInfo.action) {
      await confirmInfo.action();
    }
    setConfirmOpen(false);
  };

  // 表单对话框
  const [dialogOpen, setDialogOpen] = useState(false);
  const [isEdit, setIsEdit] = useState(false);
  const [currentSub, setCurrentSub] = useState(null);

  // 订阅类型由路由决定：/subscription/subs = 节点池，/subscription/proxy-pools = 代理池
  const activeTab = mode === 'proxy' ? 'proxy' : 'collection';
  const isProxyMode = activeTab === 'proxy';
  const [proxyDialogOpen, setProxyDialogOpen] = useState(false);
  const [proxyEditTarget, setProxyEditTarget] = useState(null);
  const [proxyFormData, setProxyFormData] = useState(createProxyFormData());

  // 表单数据
  const [formData, setFormData] = useState({
    name: '',
    description: '',
    clash: './template/clash.yaml',
    surge: './template/surge.conf',
    udp: false,
    cert: false,
    replaceServerWithHost: false,
    selectionMode: 'nodes',
    selectedNodes: [],
    selectedGroups: [],
    selectedAirports: [],
    selectedScripts: [],
    IPWhitelist: '',
    IPBlacklist: '',
    DelayTime: 0,
    MinSpeed: 0,
    UpdateInterval: 0,
    CountryWhitelist: [],
    CountryBlacklist: [],
    nodeNameRule: '',
    nodeNamePreprocess: '',
    nodeNameWhitelist: '',
    nodeNameBlacklist: '',
    tagWhitelist: '',
    tagBlacklist: '',
    protocolWhitelist: '',
    protocolBlacklist: '',
    protocolOptions: [],
    deduplicationRule: '',
    MaxFraudScore: 0,
    OnlyResidential: false,
    OnlyNative: false,
    ResidentialType: '',
    IPType: '',
    QualityStatus: '',
    UnlockProvider: '',
    UnlockStatus: '',
    UnlockKeyword: '',
    UnlockRuleMode: 'or',
    unlockRules: [],
    refreshUsageOnRequest: true // 默认开启实时获取用量信息
  });

  // 节点过滤
  const [nodeGroupFilter, setNodeGroupFilter] = useState('all');
  const [nodeSourceFilter, setNodeSourceFilter] = useState('all');
  const [nodeSearchQuery, setNodeSearchQuery] = useState('');
  const [nodeCountryFilter, setNodeCountryFilter] = useState([]);
  const [countryOptions, setCountryOptions] = useState([]);

  // 访问记录对话框
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [currentLogs, setCurrentLogs] = useState([]);

  // 排序模式
  const [sortingSubId, setSortingSubId] = useState(null);
  const [tempSortData, setTempSortData] = useState([]);
  const [selectedSortItems, setSelectedSortItems] = useState([]); // 多选排序项

  // 展开行
  const [expandedRows, setExpandedRows] = useState({});

  // 穿梭框状态
  const [checkedAvailable, setCheckedAvailable] = useState([]);
  const [checkedSelected, setCheckedSelected] = useState([]);
  const [mobileTab, setMobileTab] = useState(0);
  const [selectedNodeSearch, setSelectedNodeSearch] = useState('');
  const [namingMode, setNamingMode] = useState('builder');

  const [availableNodes, setAvailableNodes] = useState([]);
  const [availableNodesTotal, setAvailableNodesTotal] = useState(0);
  const [availableNodesLoading, setAvailableNodesLoading] = useState(false);
  const [selectedNodeMap, setSelectedNodeMap] = useState({});
  const [groupNodeCounts, setGroupNodeCounts] = useState({});
  const [allNodeTotal, setAllNodeTotal] = useState(0);

  // 预览状态
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewData, setPreviewData] = useState(null);

  // 分页
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(() => {
    const saved = localStorage.getItem('subscriptions_rowsPerPage');
    return saved ? parseInt(saved, 10) : 10;
  });
  const [totalItems, setTotalItems] = useState(0);
  const [subscriptionSearch, setSubscriptionSearch] = useState('');

  const [snackbar, setSnackbar] = useState({ open: false, message: '', severity: 'success' });

  const [groupOptions, setGroupOptions] = useState([]);
  const [airportOptions, setAirportOptions] = useState([]);
  const [sourceOptions, setSourceOptions] = useState([]);
  const [tagOptions, setTagOptions] = useState([]);
  const [protocolOptions, setProtocolOptions] = useState([]);

  const getAirportId = useCallback((airport) => Number(airport?.id ?? airport?.ID), []);
  const getAirportName = useCallback((airport) => airport?.name || airport?.Name || '', []);
  const normalizeAirportList = useCallback((data) => {
    const items = data?.items || (Array.isArray(data) ? data : []);
    return items
      .map((airport) => ({ ...airport, id: Number(airport?.id ?? airport?.ID), name: airport?.name || airport?.Name || '' }))
      .filter((airport) => Number.isInteger(airport.id) && airport.id > 0 && airport.name);
  }, []);

  const buildNodeFilterParams = useCallback(
    () => ({
      search: nodeSearchQuery,
      group: nodeGroupFilter === 'all' ? '' : nodeGroupFilter,
      source: nodeSourceFilter === 'all' ? '' : nodeSourceFilter,
      'countries[]': nodeCountryFilter,
      'excludeIds[]': formData.selectedNodes
    }),
    [formData.selectedNodes, nodeCountryFilter, nodeGroupFilter, nodeSearchQuery, nodeSourceFilter]
  );

  const buildSelectorParams = useCallback(
    () => ({
      ...buildNodeFilterParams(),
      page: 1,
      pageSize: 100
    }),
    [buildNodeFilterParams]
  );

  const hydrateSelectedNodeMap = useCallback((items) => {
    if (!Array.isArray(items) || items.length === 0) return;
    setSelectedNodeMap((prev) => {
      const next = { ...prev };
      items.forEach((item) => {
        next[item.ID] = item;
      });
      return next;
    });
  }, []);

  const fetchNodeSelector = useCallback(async () => {
    setAvailableNodesLoading(true);
    try {
      const response = await getNodeSelector(buildSelectorParams());
      const items = response.data?.items || [];
      setAvailableNodes(items);
      setAvailableNodesTotal(response.data?.total || items.length);
      hydrateSelectedNodeMap(items);
    } catch (error) {
      console.error(error);
    } finally {
      setAvailableNodesLoading(false);
    }
  }, [buildSelectorParams, hydrateSelectedNodeMap]);

  const refreshNodeSelector = useCallback(() => {
    if (!dialogOpen || formData.selectionMode === 'groups') return;
    void fetchNodeSelector();
  }, [dialogOpen, fetchNodeSelector, formData.selectionMode]);

  const trimmedSubscriptionSearch = subscriptionSearch.trim();

  // 搜索同时匹配名称与稳定唯一键（unique_key 在重命名后保持不变）
  const displayedSubscriptions = useMemo(() => {
    if (!trimmedSubscriptionSearch) return subscriptions;
    const keyword = trimmedSubscriptionSearch.toLowerCase();
    return subscriptions.filter(
      (sub) => (sub.Name || '').toLowerCase().includes(keyword) || (sub.UniqueKey || '').toLowerCase().includes(keyword)
    );
  }, [subscriptions, trimmedSubscriptionSearch]);

  const fetchSelectedNodeDetails = useCallback(
    async (ids) => {
      const validIds = Array.from(new Set((ids || []).map((id) => Number(id)).filter((id) => Number.isInteger(id) && id > 0)));
      if (validIds.length === 0) return;
      const missingIds = validIds.filter((id) => !selectedNodeMap[id]);
      if (missingIds.length === 0) return;
      try {
        const response = await getNodeSelectorByIds({ 'ids[]': missingIds });
        hydrateSelectedNodeMap(response.data || []);
      } catch (error) {
        console.error(error);
      }
    },
    [hydrateSelectedNodeMap, selectedNodeMap]
  );

  const syncSelectedNodeMapFromList = useCallback((nodes) => {
    if (!Array.isArray(nodes) || nodes.length === 0) return;
    setSelectedNodeMap((prev) => {
      const next = { ...prev };
      nodes.forEach((node) => {
        next[node.ID] = {
          ID: node.ID,
          Name: getNodeDisplayName(node),
          Group: node.Group,
          Source: node.Source,
          LinkCountry: node.LinkCountry,
          UnlockSummary: node.UnlockSummary,
          UnlockCheckAt: node.UnlockCheckAt
        };
      });
      return next;
    });
  }, []);

  // 获取订阅列表（分页）
  const fetchSubscriptions = async (currentPage, currentPageSize) => {
    setLoading(true);
    try {
      const response = await getSubscriptions({ page: currentPage + 1, pageSize: currentPageSize, type: activeTab });
      // 处理分页响应
      if (response.data && response.data.items !== undefined) {
        setSubscriptions(response.data.items || []);
        setTotalItems(response.data.total || 0);
      } else {
        // 向后兼容：老格式直接返回数组
        setSubscriptions(response.data || []);
        setTotalItems((response.data || []).length);
      }
    } catch (error) {
      console.error(error);
      showMessage(error.message || t('subscriptions.page.messages.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  };

  // 获取其他数据（分层加载）
  const fetchOtherData = useCallback(async () => {
    try {
      if (isProxyMode) {
        // 代理池页面只需要机场下拉，避免拉取模板、脚本和节点元数据。
        const airportsRes = await getAirports();
        setAirportOptions(normalizeAirportList(airportsRes.data));
        return;
      }
      const [
        templatesRes,
        scriptsRes,
        countriesRes,
        groupsRes,
        airportsRes,
        sourcesRes,
        tagsRes,
        protocolMetaRes,
        nodeCheckMetaRes,
        groupStatsRes
      ] = await Promise.all([
        getTemplates(),
        getScripts(),
        getNodeCountries(),
        getNodeGroups(),
        getAirports(),
        getNodeSources(),
        getTags(),
        getProtocolUIMeta(),
        getNodeCheckMeta(),
        getNodeGroupStats()
      ]);
      setTemplates(templatesRes.data || []);
      setScripts(scriptsRes.data || []);
      setCountryOptions(countriesRes.data || []);
      setGroupOptions((groupsRes.data || []).sort());
      setAirportOptions(normalizeAirportList(airportsRes.data));
      setSourceOptions((sourcesRes.data || []).sort());
      setTagOptions(tagsRes.data || []);
      setProtocolOptions(getRegisteredProtocolNames(protocolMetaRes.data || []));
      setUnlockMeta(nodeCheckMetaRes.data || {});
      const counts = groupStatsRes.data || {};
      setGroupNodeCounts(counts);
      setAllNodeTotal(Object.values(counts).reduce((sum, value) => sum + Number(value || 0), 0));
    } catch (error) {
      console.error(error);
    }
  }, [normalizeAirportList, isProxyMode]);

  // 初始加载
  useEffect(() => {
    fetchOtherData();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // 页签切换或首次加载时拉取对应类型的订阅
  useEffect(() => {
    setPage(0);
    fetchSubscriptions(0, rowsPerPage);
  }, [activeTab]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    refreshNodeSelector();
  }, [refreshNodeSelector, nodeGroupFilter, nodeSourceFilter, nodeSearchQuery, nodeCountryFilter, formData.selectedNodes]);

  const showMessage = useCallback((message, severity = 'success') => {
    setSnackbar({ open: true, message, severity });
  }, []);

  const copyToClipboard = async (text) => {
    try {
      // 优先使用现代 Clipboard API
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        showMessage(t('common.copied'));
        return;
      }
      // 备用方案：使用传统的 execCommand
      const textArea = document.createElement('textarea');
      textArea.value = text;
      textArea.style.position = 'fixed';
      textArea.style.left = '-999999px';
      textArea.style.top = '-999999px';
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      const successful = document.execCommand('copy');
      document.body.removeChild(textArea);
      if (successful) {
        showMessage(t('common.copied'));
      } else {
        showMessage(t('common.copyFailedManual'), 'error');
      }
    } catch (error) {
      console.error('复制失败:', error);
      showMessage(t('common.copyFailedManual'), 'error');
    }
  };

  // === 订阅操作 ===
  const handleAdd = () => {
    setIsEdit(false);
    setCurrentSub(null);
    setFormData({
      name: '',
      description: '',
      clash: './template/clash.yaml',
      surge: './template/surge.conf',
      udp: false,
      cert: false,
      replaceServerWithHost: false,
      selectionMode: 'nodes',
      selectedNodes: [],
      selectedGroups: [],
      selectedAirports: [],
      selectedScripts: [],
      IPWhitelist: '',
      IPBlacklist: '',
      DelayTime: 0,
      MinSpeed: 0,
      UpdateInterval: 0,
      CountryWhitelist: [],
      CountryBlacklist: [],
      nodeNameRule: '',
      nodeNamePreprocess: '',
      nodeNameWhitelist: '',
      nodeNameBlacklist: '',
      tagWhitelist: '',
      tagBlacklist: '',
      protocolWhitelist: '',
      protocolBlacklist: '',
      protocolOptions: protocolOptions,
      deduplicationRule: '',
      MaxFraudScore: 0,
      OnlyResidential: false,
      OnlyNative: false,
      ResidentialType: '',
      IPType: '',
      QualityStatus: '',
      UnlockProvider: '',
      UnlockStatus: '',
      UnlockKeyword: '',
      UnlockRuleMode: 'or',
      unlockRules: [],
      refreshUsageOnRequest: true
    });
    setNodeGroupFilter('all');
    setNodeSourceFilter('all');
    setNodeSearchQuery('');
    setNodeCountryFilter([]);
    setAvailableNodes([]);
    setAvailableNodesTotal(0);
    setSelectedNodeMap({});
    setDialogOpen(true);
    refreshNodeSelector();
  };

  const handleAddProxy = () => {
    setProxyEditTarget(null);
    setProxyFormData(createProxyFormData());
    setProxyDialogOpen(true);
  };

  const handleEdit = (sub) => {
    if ((sub.Type || 'collection') === 'proxy') {
      setProxyEditTarget(sub);
      setProxyFormData({
        name: sub.Name || '',
        description: sub.Description || '',
        AirportID: sub.AirportID ? Number(sub.AirportID) : ''
      });
      setProxyDialogOpen(true);
      return;
    }
    setIsEdit(true);
    setCurrentSub(sub);
    const config = typeof sub.Config === 'string' ? JSON.parse(sub.Config) : sub.Config;
    const parsedUnlockRules = (() => {
      if (!sub.UnlockRules) return [];
      try {
        return normalizeUnlockRules(typeof sub.UnlockRules === 'string' ? JSON.parse(sub.UnlockRules) : sub.UnlockRules);
      } catch {
        return [];
      }
    })();

    const nodes = sub.Nodes?.map((n) => n.ID) || [];
    const groups = (sub.Groups || []).map((g) => (typeof g === 'string' ? g : g.Name));
    const airports = (sub.Airports || []).map((airport) => getAirportId(airport)).filter((id) => Number.isInteger(id) && id > 0);
    const scriptIds = (sub.Scripts || []).map((s) => s.id);

    let mode = 'nodes';
    if (nodes.length > 0 && (groups.length > 0 || airports.length > 0)) {
      mode = 'mixed';
    } else if (groups.length > 0 || airports.length > 0) {
      mode = 'groups';
    }

    setFormData({
      name: sub.Name,
      description: sub.Description || '',
      clash: config?.clash || './template/clash.yaml',
      surge: config?.surge || './template/surge.conf',
      udp: config?.udp || false,
      cert: config?.cert || false,
      replaceServerWithHost: config?.replaceServerWithHost || false,
      selectionMode: mode,
      selectedNodes: nodes,
      selectedGroups: groups,
      selectedAirports: airports,
      selectedScripts: scriptIds,
      IPWhitelist: sub.IPWhitelist || '',
      IPBlacklist: sub.IPBlacklist || '',
      DelayTime: sub.DelayTime || 0,
      MinSpeed: sub.MinSpeed || 0,
      UpdateInterval: sub.UpdateInterval || 0,
      CountryWhitelist: sub.CountryWhitelist ? sub.CountryWhitelist.split(',').filter((c) => c.trim()) : [],
      CountryBlacklist: sub.CountryBlacklist ? sub.CountryBlacklist.split(',').filter((c) => c.trim()) : [],
      nodeNameRule: sub.NodeNameRule || '',
      nodeNamePreprocess: sub.NodeNamePreprocess || '',
      nodeNameWhitelist: sub.NodeNameWhitelist || '',
      nodeNameBlacklist: sub.NodeNameBlacklist || '',
      tagWhitelist: sub.TagWhitelist || '',
      tagBlacklist: sub.TagBlacklist || '',
      protocolWhitelist: sub.ProtocolWhitelist || '',
      protocolBlacklist: sub.ProtocolBlacklist || '',
      protocolOptions: protocolOptions,
      deduplicationRule: sub.DeduplicationRule || '',
      MaxFraudScore: sub.MaxFraudScore || 0,
      OnlyResidential: sub.OnlyResidential || false,
      OnlyNative: sub.OnlyNative || false,
      ResidentialType: sub.ResidentialType || (sub.OnlyResidential ? 'residential' : ''),
      IPType: sub.IPType || (sub.OnlyNative ? 'native' : ''),
      QualityStatus: sub.QualityStatus || '',
      UnlockProvider: sub.UnlockProvider || '',
      UnlockStatus: sub.UnlockStatus || '',
      UnlockKeyword: sub.UnlockKeyword || '',
      UnlockRuleMode: sub.UnlockRuleMode || 'or',
      unlockRules:
        parsedUnlockRules.length > 0
          ? parsedUnlockRules
          : sub.UnlockProvider || sub.UnlockStatus || sub.UnlockKeyword
            ? [{ provider: sub.UnlockProvider || '', status: sub.UnlockStatus || '', keyword: sub.UnlockKeyword || '' }]
            : [],
      refreshUsageOnRequest: sub.RefreshUsageOnRequest !== false // 默认 true
    });
    setNodeGroupFilter('all');
    setNodeSourceFilter('all');
    setNodeSearchQuery('');
    setNodeCountryFilter([]);
    syncSelectedNodeMapFromList(sub.Nodes || []);
    setDialogOpen(true);
    refreshNodeSelector();
  };

  const handleDelete = async (sub) => {
    openConfirm(t('subscriptions.page.confirm.deleteTitle'), t('subscriptions.page.confirm.deleteOne', { name: sub.Name }), async () => {
      try {
        await deleteSubscription({ id: sub.ID });
        showMessage(t('subscriptions.page.messages.deleteSuccess'));
        fetchSubscriptions(page, rowsPerPage);
      } catch (error) {
        console.error(error);
        showMessage(error.message || t('subscriptions.page.messages.deleteFailed'), 'error');
      }
    });
  };

  // 复制订阅
  const handleCopy = async (sub) => {
    openConfirm(t('subscriptions.page.confirm.copyTitle'), t('subscriptions.page.confirm.copyOne', { name: sub.Name }), async () => {
      try {
        await copySubscription(sub.ID);
        showMessage(t('subscriptions.page.messages.copySuccess'));
        fetchSubscriptions(page, rowsPerPage);
      } catch (error) {
        console.error(error);
        showMessage(error.message || t('subscriptions.page.messages.copyFailed'), 'error');
      }
    });
  };

  const handleSubmit = async () => {
    if (!formData.name.trim()) {
      showMessage(t('subscriptions.page.messages.nameRequired'), 'warning');
      return;
    }

    try {
      const config = JSON.stringify({
        clash: formData.clash,
        surge: formData.surge,
        udp: formData.udp,
        cert: formData.cert,
        replaceServerWithHost: formData.replaceServerWithHost
      });

      const requestData = {
        name: formData.name.trim(),
        description: formData.description || '',
        config,
        IPWhitelist: formData.IPWhitelist,
        IPBlacklist: formData.IPBlacklist,
        DelayTime: formData.DelayTime,
        MinSpeed: formData.MinSpeed,
        UpdateInterval: Math.min(8760, Math.max(0, Number(formData.UpdateInterval) || 0)),
        scripts: formData.selectedScripts.join(','),
        CountryWhitelist: formData.CountryWhitelist.join(','),
        CountryBlacklist: formData.CountryBlacklist.join(','),
        NodeNameRule: formData.nodeNameRule,
        NodeNamePreprocess: formData.nodeNamePreprocess,
        NodeNameWhitelist: formData.nodeNameWhitelist,
        NodeNameBlacklist: formData.nodeNameBlacklist,
        TagWhitelist: formData.tagWhitelist,
        TagBlacklist: formData.tagBlacklist,
        ProtocolWhitelist: formData.protocolWhitelist,
        ProtocolBlacklist: formData.protocolBlacklist,
        DeduplicationRule: formData.deduplicationRule || '',
        MaxFraudScore: formData.MaxFraudScore,
        OnlyResidential: formData.ResidentialType === 'residential',
        OnlyNative: formData.IPType === 'native',
        ResidentialType: formData.ResidentialType || '',
        IPType: formData.IPType || '',
        QualityStatus: formData.QualityStatus || '',
        UnlockProvider: '',
        UnlockStatus: '',
        UnlockKeyword: '',
        UnlockRuleMode: formData.UnlockRuleMode || 'or',
        UnlockRules: buildUnlockRulesPayload(formData.unlockRules),
        RefreshUsageOnRequest: formData.refreshUsageOnRequest
      };

      if (formData.selectionMode === 'nodes') {
        requestData.nodeIds = formData.selectedNodes.join(',');
        requestData.groups = '';
        requestData.airports = '';
      } else if (formData.selectionMode === 'groups') {
        requestData.nodeIds = '';
        requestData.groups = formData.selectedGroups.join(',');
        requestData.airports = formData.selectedAirports.join(',');
      } else {
        requestData.nodeIds = formData.selectedNodes.join(',');
        requestData.groups = formData.selectedGroups.join(',');
        requestData.airports = formData.selectedAirports.join(',');
      }

      if (isEdit) {
        // 优先用稳定唯一键定位记录，重命名后接口仍指向同一条节点池
        requestData.unique_key = currentSub.UniqueKey || '';
        requestData.oldname = currentSub.Name;
        await updateSubscription(requestData);
        showMessage(t('subscriptions.page.messages.updateSuccess'));
      } else {
        await addSubscription(requestData);
        showMessage(t('subscriptions.page.messages.addSuccess'));
      }
      setDialogOpen(false);
      fetchSubscriptions(page, rowsPerPage);
    } catch (error) {
      console.error(error);
      showMessage(
        error.message || (isEdit ? t('subscriptions.page.messages.updateFailed') : t('subscriptions.page.messages.addFailed')),
        'error'
      );
    }
  };

  // 代理订阅提交：只保存名称与要转发的机场，地址与请求配置取自机场本身
  const handleProxySubmit = async () => {
    if (!proxyFormData.name.trim()) {
      showMessage(t('subscriptions.page.messages.nameRequired'), 'warning');
      return;
    }
    if (!proxyFormData.AirportID) {
      showMessage(t('subscriptions.page.messages.airportRequired'), 'warning');
      return;
    }

    try {
      const requestData = {
        type: 'proxy',
        name: proxyFormData.name.trim(),
        description: proxyFormData.description || '',
        AirportID: Number(proxyFormData.AirportID),
        RefreshUsageOnRequest: false
      };

      if (proxyEditTarget) {
        requestData.unique_key = proxyEditTarget.UniqueKey || '';
        requestData.oldname = proxyEditTarget.Name;
        await updateSubscription(requestData);
        showMessage(t('subscriptions.page.messages.updateSuccess'));
      } else {
        await addSubscription(requestData);
        showMessage(t('subscriptions.page.messages.addSuccess'));
      }
      setProxyDialogOpen(false);
      fetchSubscriptions(page, rowsPerPage);
    } catch (error) {
      console.error(error);
      showMessage(
        error.message || (proxyEditTarget ? t('subscriptions.page.messages.updateFailed') : t('subscriptions.page.messages.addFailed')),
        'error'
      );
    }
  };

  // 节点选择操作（使用 node.ID）
  const handleAddNode = (nodeId) => {
    if (formData.selectedNodes.includes(nodeId)) return;
    const nextSelected = [...formData.selectedNodes, nodeId];
    setFormData({ ...formData, selectedNodes: nextSelected });
    setCheckedAvailable(checkedAvailable.filter((id) => id !== nodeId));
    void fetchSelectedNodeDetails(nextSelected);
  };

  const handleRemoveNode = (nodeId) => {
    setFormData({ ...formData, selectedNodes: formData.selectedNodes.filter((id) => id !== nodeId) });
    setCheckedSelected(checkedSelected.filter((id) => id !== nodeId));
  };

  const selectedNodesList = useMemo(() => {
    return formData.selectedNodes.map((id) => selectedNodeMap[id]).filter(Boolean);
  }, [formData.selectedNodes, selectedNodeMap]);

  const availableNodeCount = availableNodesTotal || availableNodes.length;

  const handleAddAllVisible = async () => {
    try {
      const response = await getNodeIds(buildNodeFilterParams());
      const matchedIds = response.data || [];
      const newNodes = Array.from(new Set([...formData.selectedNodes, ...matchedIds]));
      setFormData({ ...formData, selectedNodes: newNodes });
      setCheckedAvailable([]);
      await fetchSelectedNodeDetails(newNodes);
    } catch (error) {
      console.error(error);
      showMessage(error.message || 'Failed to add nodes in bulk', 'error');
    }
  };

  const handleRemoveAll = () => {
    setFormData({ ...formData, selectedNodes: [] });
    setCheckedSelected([]);
  };

  // 多选操作（使用 node.ID）
  const handleToggleAvailable = (nodeId) => {
    if (checkedAvailable.includes(nodeId)) {
      setCheckedAvailable(checkedAvailable.filter((id) => id !== nodeId));
    } else {
      setCheckedAvailable([...checkedAvailable, nodeId]);
    }
  };

  const handleToggleSelected = (nodeId) => {
    if (checkedSelected.includes(nodeId)) {
      setCheckedSelected(checkedSelected.filter((id) => id !== nodeId));
    } else {
      setCheckedSelected([...checkedSelected, nodeId]);
    }
  };

  const handleAddChecked = () => {
    const newNodes = Array.from(new Set([...formData.selectedNodes, ...checkedAvailable]));
    setFormData({ ...formData, selectedNodes: newNodes });
    setCheckedAvailable([]);
    void fetchSelectedNodeDetails(newNodes);
  };

  const handleRemoveChecked = () => {
    const newNodes = formData.selectedNodes.filter((id) => !checkedSelected.includes(id));
    setFormData({ ...formData, selectedNodes: newNodes });
    setCheckedSelected([]);
  };

  const handleToggleAllAvailable = async () => {
    if (checkedAvailable.length === availableNodeCount && availableNodeCount > 0) {
      setCheckedAvailable([]);
      return;
    }
    try {
      const response = await getNodeIds(buildNodeFilterParams());
      setCheckedAvailable(response.data || []);
    } catch (error) {
      console.error(error);
      showMessage(error.message || 'Failed to select nodes in bulk', 'error');
    }
  };

  const handleToggleAllSelected = () => {
    if (checkedSelected.length === selectedNodesList.length) {
      setCheckedSelected([]);
    } else {
      setCheckedSelected(selectedNodesList.map((n) => n.ID));
    }
  };

  // 预览节点
  const handlePreview = async () => {
    setPreviewLoading(true);
    try {
      // 构建预览请求数据
      const previewRequest = {
        NodeIDs: formData.selectionMode !== 'groups' ? formData.selectedNodes : [],
        Groups: formData.selectionMode !== 'nodes' ? formData.selectedGroups : [],
        AirportIDs: formData.selectionMode !== 'nodes' ? formData.selectedAirports : [],
        Scripts: formData.selectedScripts || [],
        DelayTime: formData.DelayTime || 0,
        MinSpeed: formData.MinSpeed || 0,
        CountryWhitelist: formData.CountryWhitelist.join(','),
        CountryBlacklist: formData.CountryBlacklist.join(','),
        TagWhitelist: formData.tagWhitelist || '',
        TagBlacklist: formData.tagBlacklist || '',
        ProtocolWhitelist: formData.protocolWhitelist || '',
        ProtocolBlacklist: formData.protocolBlacklist || '',
        NodeNameWhitelist: formData.nodeNameWhitelist || '',
        NodeNameBlacklist: formData.nodeNameBlacklist || '',
        MaxFraudScore: formData.MaxFraudScore || 0,
        OnlyResidential: formData.ResidentialType === 'residential',
        OnlyNative: formData.IPType === 'native',
        ResidentialType: formData.ResidentialType || '',
        IPType: formData.IPType || '',
        QualityStatus: formData.QualityStatus || '',
        UnlockProvider: '',
        UnlockStatus: '',
        UnlockKeyword: '',
        UnlockRuleMode: formData.UnlockRuleMode || 'or',
        UnlockRules: buildUnlockRulesPayload(formData.unlockRules),
        NodeNamePreprocess: formData.nodeNamePreprocess || '',
        NodeNameRule: formData.nodeNameRule || '',
        DeduplicationRule: formData.deduplicationRule || ''
      };

      const response = await previewSubscriptionNodes(previewRequest);
      // 成功（code === 200 时返回，否则被拦截器 reject）
      setPreviewData(response.data);
      setPreviewOpen(true);
    } catch (error) {
      console.error(error);
      showMessage(error.message || 'Preview request failed', 'error');
    } finally {
      setPreviewLoading(false);
    }
  };

  // 预览已保存的订阅（从列表触发）
  // 使用 SubscriptionID 让后端直接调用 GetSub 逻辑，确保预览与实际拉取结果一致
  const handlePreviewSubscription = async (sub) => {
    setPreviewLoading(true);
    try {
      const previewRequest = {
        SubscriptionID: sub.ID // 使用订阅ID，后端会调用 GetSub 获取完整节点列表
      };

      const response = await previewSubscriptionNodes(previewRequest);
      // 成功（code === 200 时返回，否则被拦截器 reject）
      setPreviewData(response.data);
      setPreviewOpen(true);
    } catch (error) {
      console.error(error);
      showMessage(error.message || 'Preview request failed', 'error');
    } finally {
      setPreviewLoading(false);
    }
  };

  // === 分组排序 ===
  const [groupSortOpen, setGroupSortOpen] = useState(false);

  // === 链式代理 ===
  const [chainProxyOpen, setChainProxyOpen] = useState(false);
  const [chainProxySub, setChainProxySub] = useState(null);

  const handleChainProxy = (sub) => {
    setChainProxySub(sub);
    setChainProxyOpen(true);
  };

  // === 访问记录 ===
  const handleLogs = (sub) => {
    setCurrentLogs(sub.SubLogs || []);
    setLogsDialogOpen(true);
  };

  // === 排序功能 ===
  const handleStartSort = (sub) => {
    setSortingSubId(sub.ID);
    setSelectedSortItems([]); // 重置多选
    const sortData = [];
    (sub.Nodes || []).forEach((node, idx) => {
      sortData.push({
        ID: node.ID,
        Name: getNodeDisplayName(node),
        SortKey: `node:${node.ID}`,
        Sort: node.Sort !== undefined ? node.Sort : idx,
        IsGroup: false,
        IsAirport: false
      });
    });
    (sub.Groups || []).forEach((group, idx) => {
      const g = typeof group === 'string' ? { Name: group, Sort: sub.Nodes?.length + idx } : group;
      sortData.push({
        ID: 0,
        Name: g.Name,
        SortKey: `group:${g.Name}`,
        Sort: g.Sort !== undefined ? g.Sort : sub.Nodes?.length + idx,
        IsGroup: true,
        IsAirport: false
      });
    });
    (sub.Airports || []).forEach((airport, idx) => {
      const airportId = getAirportId(airport);
      const fallbackSort = (sub.Nodes?.length || 0) + (sub.Groups?.length || 0) + idx;
      sortData.push({
        ID: airportId,
        Name: getAirportName(airport),
        SortKey: `airport:${airportId}`,
        Sort: airport.Sort !== undefined ? airport.Sort : fallbackSort,
        IsGroup: false,
        IsAirport: true
      });
    });
    sortData.sort((a, b) => a.Sort - b.Sort);
    setTempSortData(sortData);
    showMessage(t('subscriptions.page.messages.sortModeStarted'), 'info');
  };

  const handleConfirmSort = async (sub) => {
    const newSortData = tempSortData.map((item, idx) => ({ ...item, Sort: idx }));
    try {
      await sortSubscription({
        ID: sub.ID,
        NodeSort: newSortData
      });
      showMessage(t('subscriptions.page.messages.sortUpdated'));
      setSortingSubId(null);
      setTempSortData([]);
      fetchSubscriptions(page, rowsPerPage);
    } catch (error) {
      console.error(error);
      showMessage(error.message || t('subscriptions.page.messages.sortSaveFailed'), 'error');
    }
  };

  const handleCancelSort = () => {
    setSortingSubId(null);
    setTempSortData([]);
    setSelectedSortItems([]);
    showMessage(t('subscriptions.page.messages.sortCancelled'), 'info');
  };

  const onDragEnd = (result) => {
    if (!result.destination) return;
    const items = Array.from(tempSortData);
    const [reorderedItem] = items.splice(result.source.index, 1);
    items.splice(result.destination.index, 0, reorderedItem);
    setTempSortData(items);
  };

  // === 多选排序功能 ===
  const handleToggleSortSelect = (name) => {
    setSelectedSortItems((prev) => (prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name]));
  };

  const handleSelectAllSort = () => {
    setSelectedSortItems(tempSortData.map((item) => item.SortKey || item.Name));
  };

  const handleClearSortSelection = () => {
    setSelectedSortItems([]);
  };

  // 批量排序（调用后端API）
  const handleBatchSort = async (sortBy, sortOrder) => {
    if (!sortingSubId) return;
    try {
      await batchSortSubscription({
        ID: sortingSubId,
        sortBy,
        sortOrder
      });
      showMessage(t('subscriptions.page.messages.batchSortSuccess'));
      // 重新加载订阅数据并刷新排序列表
      const response = await getSubscriptions({ page: page + 1, pageSize: rowsPerPage });
      const subs = response.data?.items || response.data || [];
      setSubscriptions(subs);
      // 找到当前排序的订阅并刷新排序数据
      const currentSub = subs.find((s) => s.ID === sortingSubId);
      if (currentSub) {
        handleStartSort(currentSub);
      }
    } catch (error) {
      console.error(error);
      showMessage(error.message || t('subscriptions.page.messages.batchSortFailed'), 'error');
    }
  };

  // 批量移动（前端本地操作，需要用户确认后保存）
  const handleBatchMove = (targetIndex) => {
    if (selectedSortItems.length === 0) return;

    const selected = tempSortData.filter((item) => selectedSortItems.includes(item.SortKey || item.Name));
    const remaining = tempSortData.filter((item) => !selectedSortItems.includes(item.SortKey || item.Name));

    // 插入到目标位置
    const newData = [...remaining];
    const insertAt = Math.min(Math.max(0, targetIndex), newData.length);
    newData.splice(insertAt, 0, ...selected);

    setTempSortData(newData);
    setSelectedSortItems([]);
    showMessage(t('subscriptions.page.messages.movedItems', { count: selected.length, position: insertAt + 1 }));
  };

  // 展开/折叠行
  const toggleRow = (subId) => {
    setExpandedRows({ ...expandedRows, [subId]: !expandedRows[subId] });
  };

  const getSortedItems = (sub) => {
    const items = [];
    (sub.Nodes || []).forEach((node, idx) => {
      items.push({
        ...node,
        _type: 'node',
        _sort: node.Sort !== undefined ? node.Sort : idx
      });
    });
    (sub.Groups || []).forEach((group, idx) => {
      const g = typeof group === 'string' ? { Name: group } : group;
      items.push({
        ...g,
        _type: 'group',
        _sort: g.Sort !== undefined ? g.Sort : (sub.Nodes?.length || 0) + idx
      });
    });
    (sub.Airports || []).forEach((airport, idx) => {
      const fallbackSort = (sub.Nodes?.length || 0) + (sub.Groups?.length || 0) + idx;
      items.push({
        ...airport,
        ID: getAirportId(airport),
        Name: getAirportName(airport),
        _type: 'airport',
        _sort: airport.Sort !== undefined ? airport.Sort : fallbackSort
      });
    });
    return items.sort((a, b) => a._sort - b._sort);
  };

  return (
    <MainCard
      title={isProxyMode ? t('proxyPools.page.title') : t('subscriptions.page.title')}
      secondary={
        matchDownMd ? (
          <Stack direction="row" spacing={1}>
            {!isProxyMode && (
              <Tooltip title={t('subscriptions.page.actions.groupSort')}>
                <IconButton onClick={() => setGroupSortOpen(true)} size="small">
                  <CategoryIcon />
                </IconButton>
              </Tooltip>
            )}
            <Tooltip title={t('common.refresh')}>
              <IconButton onClick={() => fetchSubscriptions(page, rowsPerPage)} disabled={loading} size="small">
                <RefreshIcon
                  sx={
                    loading
                      ? {
                          animation: 'spin 1s linear infinite',
                          '@keyframes spin': { from: { transform: 'rotate(0deg)' }, to: { transform: 'rotate(360deg)' } }
                        }
                      : {}
                  }
                />
              </IconButton>
            </Tooltip>
            <Button variant="contained" startIcon={<AddIcon />} disableRipple onClick={isProxyMode ? handleAddProxy : handleAdd}>
              {t('common.add')}
            </Button>
          </Stack>
        ) : (
          <Stack direction="row" spacing={1}>
            {!isProxyMode && (
              <Button variant="outlined" startIcon={<CategoryIcon />} onClick={() => setGroupSortOpen(true)}>
                {t('subscriptions.page.actions.groupSort')}
              </Button>
            )}
            <Button variant="contained" startIcon={<AddIcon />} disableRipple onClick={isProxyMode ? handleAddProxy : handleAdd}>
              {isProxyMode ? t('subscriptions.page.actions.addProxySubscription') : t('subscriptions.page.actions.addSubscription')}
            </Button>
            <IconButton onClick={() => fetchSubscriptions(page, rowsPerPage)} disabled={loading}>
              <RefreshIcon
                sx={
                  loading
                    ? {
                        animation: 'spin 1s linear infinite',
                        '@keyframes spin': { from: { transform: 'rotate(0deg)' }, to: { transform: 'rotate(360deg)' } }
                      }
                    : {}
                }
              />
            </IconButton>
          </Stack>
        )
      }
    >
      {isProxyMode && (
        <Alert severity="info" variant="outlined" sx={{ mb: 2 }}>
          {t('subscriptions.page.tabs.proxyHint')}
        </Alert>
      )}

      <Stack spacing={1} sx={{ mb: 2 }}>
        <TextField
          size="small"
          fullWidth={matchDownMd}
          label={isProxyMode ? t('proxyPools.page.searchLabel') : t('subscriptions.page.search.label')}
          placeholder={isProxyMode ? t('proxyPools.page.searchPlaceholder') : t('subscriptions.page.search.placeholder')}
          value={subscriptionSearch}
          onChange={(event) => setSubscriptionSearch(event.target.value)}
          sx={{ width: { xs: '100%', md: 460 } }}
          InputProps={{
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon color="action" fontSize="small" />
              </InputAdornment>
            ),
            endAdornment: trimmedSubscriptionSearch && (
              <InputAdornment position="end">
                <IconButton size="small" onClick={() => setSubscriptionSearch('')} edge="end" aria-label={t('common.clear')}>
                  <ClearIcon fontSize="small" />
                </IconButton>
              </InputAdornment>
            )
          }}
        />
      </Stack>

      {!loading && displayedSubscriptions.length === 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {isProxyMode ? t('proxyPools.page.empty') : t('subscriptions.page.empty')}
        </Alert>
      )}

      {matchDownMd ? (
        <SubscriptionMobileCard
          subscriptions={displayedSubscriptions}
          isProxyTab={isProxyMode}
          airportOptions={airportOptions}
          page={page}
          rowsPerPage={rowsPerPage}
          expandedRows={expandedRows}
          sortingSubId={sortingSubId}
          tempSortData={tempSortData}
          selectedSortItems={selectedSortItems}
          theme={theme}
          onToggleRow={toggleRow}
          onLogs={handleLogs}
          onEdit={handleEdit}
          onDelete={handleDelete}
          onCopy={handleCopy}
          onPreview={handlePreviewSubscription}
          onChainProxy={handleChainProxy}
          onStartSort={handleStartSort}
          onConfirmSort={handleConfirmSort}
          onCancelSort={handleCancelSort}
          onDragEnd={onDragEnd}
          onCopyToClipboard={copyToClipboard}
          getSortedItems={getSortedItems}
          onToggleSortSelect={handleToggleSortSelect}
          onSelectAllSort={handleSelectAllSort}
          onClearSortSelection={handleClearSortSelection}
          onBatchSort={handleBatchSort}
          onBatchMove={handleBatchMove}
        />
      ) : (
        <SubscriptionTable
          subscriptions={displayedSubscriptions}
          isProxyTab={isProxyMode}
          airportOptions={airportOptions}
          page={page}
          rowsPerPage={rowsPerPage}
          expandedRows={expandedRows}
          sortingSubId={sortingSubId}
          tempSortData={tempSortData}
          selectedSortItems={selectedSortItems}
          onToggleRow={toggleRow}
          onLogs={handleLogs}
          onEdit={handleEdit}
          onDelete={handleDelete}
          onCopy={handleCopy}
          onPreview={handlePreviewSubscription}
          onChainProxy={handleChainProxy}
          onStartSort={handleStartSort}
          onConfirmSort={handleConfirmSort}
          onCancelSort={handleCancelSort}
          onDragEnd={onDragEnd}
          onCopyToClipboard={copyToClipboard}
          getSortedItems={getSortedItems}
          onToggleSortSelect={handleToggleSortSelect}
          onSelectAllSort={handleSelectAllSort}
          onClearSortSelection={handleClearSortSelection}
          onBatchSort={handleBatchSort}
          onBatchMove={handleBatchMove}
        />
      )}

      {!trimmedSubscriptionSearch && (
        <Pagination
          page={page}
          pageSize={rowsPerPage}
          totalItems={totalItems}
          onPageChange={(e, newPage) => {
            setPage(newPage);
            fetchSubscriptions(newPage, rowsPerPage);
          }}
          onPageSizeChange={(e) => {
            const newValue = parseInt(e.target.value, 10);
            setRowsPerPage(newValue);
            localStorage.setItem('subscriptions_rowsPerPage', newValue);
            setPage(0);
            fetchSubscriptions(0, newValue);
          }}
          pageSizeOptions={[10, 20, 50, 100]}
        />
      )}

      {/* 添加/编辑对话框 */}
      <SubscriptionFormDialog
        open={dialogOpen}
        isEdit={isEdit}
        formData={formData}
        setFormData={setFormData}
        templates={templates}
        scripts={scripts}
        selectorNodes={availableNodes}
        selectorNodesTotal={availableNodeCount}
        selectorNodesLoading={availableNodesLoading}
        selectedNodesList={selectedNodesList}
        selectedNodeSearch={selectedNodeSearch}
        setSelectedNodeSearch={setSelectedNodeSearch}
        groupNodeCounts={groupNodeCounts}
        allNodeTotal={allNodeTotal}
        groupOptions={groupOptions}
        airportOptions={airportOptions}
        sourceOptions={sourceOptions}
        countryOptions={countryOptions}
        tagOptions={tagOptions}
        nodeGroupFilter={nodeGroupFilter}
        setNodeGroupFilter={setNodeGroupFilter}
        nodeSourceFilter={nodeSourceFilter}
        setNodeSourceFilter={setNodeSourceFilter}
        nodeSearchQuery={nodeSearchQuery}
        setNodeSearchQuery={setNodeSearchQuery}
        nodeCountryFilter={nodeCountryFilter}
        setNodeCountryFilter={setNodeCountryFilter}
        checkedAvailable={checkedAvailable}
        checkedSelected={checkedSelected}
        mobileTab={mobileTab}
        setMobileTab={setMobileTab}
        namingMode={namingMode}
        setNamingMode={setNamingMode}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
        onAddNode={handleAddNode}
        onRemoveNode={handleRemoveNode}
        onAddAllVisible={handleAddAllVisible}
        onRemoveAll={handleRemoveAll}
        onToggleAvailable={handleToggleAvailable}
        onToggleSelected={handleToggleSelected}
        onAddChecked={handleAddChecked}
        onRemoveChecked={handleRemoveChecked}
        onToggleAllAvailable={handleToggleAllAvailable}
        onToggleAllSelected={handleToggleAllSelected}
        onPreview={handlePreview}
        previewLoading={previewLoading}
      />

      {/* 代理订阅对话框 */}
      <ProxySubscriptionFormDialog
        open={proxyDialogOpen}
        isEdit={Boolean(proxyEditTarget)}
        formData={proxyFormData}
        setFormData={setProxyFormData}
        airportOptions={airportOptions}
        onClose={() => setProxyDialogOpen(false)}
        onSubmit={handleProxySubmit}
      />

      {/* 访问记录对话框 */}
      <AccessLogsDialog open={logsDialogOpen} logs={currentLogs} onClose={() => setLogsDialogOpen(false)} />

      {/* 提示消息 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar({ ...snackbar, open: false })}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity}>{snackbar.message}</Alert>
      </Snackbar>

      {/* 确认对话框 */}
      <ConfirmDialog
        open={confirmOpen}
        title={confirmInfo.title}
        content={confirmInfo.content}
        onClose={handleConfirmClose}
        onConfirm={handleConfirmAction}
      />

      {/* 节点预览对话框 */}
      <NodePreviewDialog
        open={previewOpen}
        loading={previewLoading}
        data={previewData}
        tagColorMap={tagOptions.reduce((acc, tag) => {
          acc[tag.Name] = tag.Color;
          return acc;
        }, {})}
        onClose={() => setPreviewOpen(false)}
      />

      {/* 分组排序对话框 */}
      <GroupSortDialog open={groupSortOpen} onClose={() => setGroupSortOpen(false)} showMessage={showMessage} />

      {/* 链式代理配置对话框 */}
      <ChainProxyDialog open={chainProxyOpen} subscription={chainProxySub} onClose={() => setChainProxyOpen(false)} />
    </MainCard>
  );
}
