import copy
import unittest
from launch import prepare_config


class LaunchTests(unittest.TestCase):
    def test_uses_actual_free_gpu_indices_and_selected_context(self):
        config = {'args': ['--max-context', '65536']}
        state, result = prepare_config({'gpus': 2, 'ctx': 262144, 'parallel': 1}, config,
                                       [(0, 1000), (1, 23000), (2, 5000), (3, 24000)], 'test-key')
        self.assertEqual(state['gpu_ids'], [1, 3])
        self.assertEqual(result['gpu'], [1, 3])
        self.assertEqual(result['args'], ['--max-context', '262144', '--parallel', '1'])
        self.assertEqual(result['port'], 18037)

    def test_refuses_occupied_gpu_instead_of_loading_over_other_model(self):
        with self.assertRaisesRegex(ValueError, 'Need 4 free GPUs'):
            prepare_config({'gpus': 4}, {'args': []}, [(0, 24000), (1, 24000), (2, 1000), (3, 24000)], 'key')

    def test_rejects_unconfigured_launch_values(self):
        for state in [{'gpus': 5}, {'ctx': 524288}, {'parallel': 2}]:
            with self.subTest(state=state), self.assertRaises(ValueError):
                prepare_config(copy.copy(state), {'args': []}, [(i, 24000) for i in range(4)], 'key')


if __name__ == '__main__':
    unittest.main()
